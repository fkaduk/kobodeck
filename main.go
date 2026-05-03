package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	configFileFlag = flag.String("config", "", "path to the configuration file")
	checkFlag      = flag.Bool("check", false, "validate config and show what would be synced, then exit")
)

type readeckoboConfig struct {
	URL       string `toml:"URL"`
	Token     string `toml:"Token"`
	Verbose   bool   `toml:"Verbose"`
	Delete    bool   `toml:"Delete"`
	Log       string `toml:"Log"`
	Workers   int    `toml:"Workers"`
	Limit     int    `toml:"Limit"`
	Output    string `toml:"Output"`
	Timeout   int    `toml:"Timeout"`
	Labels    string `toml:"Labels"`
	Uninstall bool   `toml:"Uninstall"`
}

var config readeckoboConfig

func (c *readeckoboConfig) validate() error {
	if c.URL == "" {
		return fmt.Errorf("URL is required")
	}
	if c.Token == "" {
		return fmt.Errorf("Token is required")
	}
	if c.Output == "" {
		return fmt.Errorf("Output is required")
	}
	if c.Workers <= 0 {
		return fmt.Errorf("Workers must be greater than 0")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("Timeout must be greater than 0")
	}
	return nil
}

var (
	filesChanged atomic.Bool
	home         = os.Getenv("HOME")
	version      = "undefined"
	nickelDB     = "/mnt/onboard/.kobo/KoboReader.sqlite"
)

func main() {
	flag.Parse()
	configFile, configErr := findConfig()
	debugf("config: %#v", config)

	setupLogging(config)
	debug.SetPanicOnFault(true)

	if configErr != nil {
		log.Fatal(configErr.Error())
	}
	if err := config.validate(); err != nil {
		log.Fatal("invalid configuration: ", err)
	}
	log.Println("readeckobo version", version, "loaded configuration from", configFile)

	if *checkFlag {
		if err := runCheck(os.Stdout); err != nil {
			log.Fatal("check failed: ", err)
		}
		return
	}

	start := time.Now()
	defer func() {
		log.Printf("version %s completed in %s", version, time.Since(start).Truncate(time.Millisecond))
	}()

	if config.Uninstall {
		uninstall()
	}

	lock, err := getLock()
	if err != nil {
		log.Fatal(err)
	}
	defer lock.Close()

	log.Println("connecting to", config.URL)
	if config.Token == "" {
		log.Fatal("no Token configured; create one in the Readeck UI and add it to the config file")
	}

	client := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	sem := make(chan bool, config.Workers)
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	entries, err := listEntries()
	if err != nil {
		log.Fatal(err)
	}

	valid := make(map[string]bool)
	tags := make(map[string]bool)
	if len(config.Labels) > 0 {
		for _, tag := range strings.Split(strings.ToLower(config.Labels), ",") {
			tags[strings.TrimSpace(tag)] = true
		}
	}

OuterLoop:
	for _, entry := range entries {
		if len(tags) > 0 && !checkTags(tags, entry.Labels) {
			debugf("skipping %s (not in tags)", entry.ID)
			continue
		}
		debugln("dispatching", entry.ID)
		valid[entry.ID] = true
		select {
		case sem <- true:
			go func(e readeckBookmark) {
				defer func() { <-sem }()
				if err := download(client, e); err != nil {
					log.Println("error downloading entry", e.ID, err)
				}
			}(entry)
		case sig := <-sigc:
			log.Println("got signal:", sig, ", waiting for downloads to finish...")
			break OuterLoop
		}
	}
	for i := 0; i < cap(sem); i++ {
		sem <- true
	}

	inspectLocalFiles(config, valid)

	if config.Verbose {
		fds := listOpenFds()
		log.Printf("%d open file descriptors: %s", len(fds), fds)
	}
	const fakeConnectUSB = "/usr/local/bin/fake-connect-usb"
	if filesChanged.Load() {
		if _, err := os.Stat(fakeConnectUSB); err == nil {
			log.Println("triggering Nickel rescan")
			out, err := exec.Command(fakeConnectUSB).CombinedOutput()
			if err != nil {
				log.Println("fake-connect-usb failed:", err)
			} else if len(out) > 0 {
				log.Println(string(out))
			}
		}
	}
}

func debugln(args ...interface{}) {
	if config.Verbose {
		log.Println(args...)
	}
}

func debugf(format string, args ...interface{}) {
	if config.Verbose {
		log.Printf(format, args...)
	}
}

func setupLogging(cfg readeckoboConfig, extraWriters ...io.Writer) {
	var writers []io.Writer
	if len(cfg.Log) > 0 {
		writers = append(writers, &lumberjack.Logger{
			Filename:   cfg.Log,
			MaxSize:    1,
			MaxBackups: 7,
			MaxAge:     7,
		})
	}
	writers = append(writers, os.Stdout)
	writers = append(writers, extraWriters...)
	log.SetOutput(io.MultiWriter(writers...))
}

const confPath = "readeckobo.toml"

var confPaths = []string{
	home + "/.config/" + confPath,
	home + "/." + confPath,
	"/mnt/onboard/." + confPath,
	"/etc/" + confPath,
}

func loadConfig(path string) error {
	_, err := toml.DecodeFile(path, &config)
	return err
}

func findConfig() (string, error) {
	if *configFileFlag != "" {
		if err := loadConfig(*configFileFlag); err == nil {
			return *configFileFlag, nil
		}
	}
	for _, path := range confPaths {
		if err := loadConfig(path); err == nil {
			return path, nil
		}
		debugf("can't load config path: %v", path)
	}
	return "", fmt.Errorf("no config file found")
}

func uninstall() {
	log.Println("uninstall requested, clearing myself out")
	if !strings.HasPrefix(os.Args[0], "/usr/local") {
		log.Fatal("unexpected command path, aborting uninstall:", os.Args[0])
	}
	files := []string{
		"/etc/readeckobo.toml",
		"/etc/udev/rules.d/90-readeckobo.rules",
		"/usr/local/bin/fake-connect-usb",
		"/usr/local/bin/readeckobo-run",
		"/usr/local/bin/readeckobo",
	}
	var lastErr error
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			log.Printf("failed: %s", err)
			lastErr = err
		} else {
			log.Printf("deleted %s", file)
		}
	}
	if lastErr != nil {
		log.Fatal("uninstall partially failed")
	}
	log.Fatal("uninstall complete")
}

func getLock() (*os.File, error) {
	f, err := os.OpenFile("/tmp/readeckobo.lock", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("already running")
	}
	return f, nil
}

func runCheck(w io.Writer) error {
	fmt.Fprintln(w, "Configuration:")
	fmt.Fprintf(w, "  URL:     %s\n", config.URL)
	fmt.Fprintf(w, "  Output:  %s\n", config.Output)
	fmt.Fprintf(w, "  Workers: %d\n", config.Workers)
	fmt.Fprintf(w, "  Limit:   %d\n", config.Limit)
	fmt.Fprintf(w, "  Delete:  %v\n", config.Delete)
	if config.Labels != "" {
		fmt.Fprintf(w, "  Labels:  %s\n", config.Labels)
	} else {
		fmt.Fprintln(w, "  Labels:  (all)")
	}
	fmt.Fprintln(w)

	fmt.Fprint(w, "Connecting to Readeck... ")
	entries, err := listEntries()
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "OK")
	fmt.Fprintln(w)

	labelFilter := make(map[string]bool)
	if config.Labels != "" {
		for _, l := range strings.Split(strings.ToLower(config.Labels), ",") {
			labelFilter[strings.TrimSpace(l)] = true
		}
	}

	var matched, skipped int
	for _, entry := range entries {
		if len(labelFilter) > 0 && !checkTags(labelFilter, entry.Labels) {
			skipped++
			continue
		}
		matched++
		fmt.Fprintf(w, "  %s — %s\n", entry.ID, entry.Title)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%d bookmarks to sync", matched)
	if skipped > 0 {
		fmt.Fprintf(w, ", %d skipped (label filter)", skipped)
	}
	fmt.Fprintln(w)
	return nil
}

func inspectLocalFiles(cfg readeckoboConfig, valid map[string]bool) {
	outputDir := strings.TrimSuffix(cfg.Output, "/")
	files, _ := filepath.Glob(outputDir + "/*.epub")
	debugln("local files to inspect:", files)
	for _, file := range files {
		uid := strings.TrimSuffix(filepath.Base(file), ".epub")
		if uid == "" {
			log.Println("skipping file with empty name:", file)
			continue
		}
		status, err := readStatus(uid, outputDir)
		if err != nil {
			log.Println(err)
			continue
		}
		if status == bookRead {
			if err = markAsRead(uid); err != nil {
				log.Println("failed to mark as read:", err)
			} else {
				valid[uid] = false
			}
		}
		if cfg.Delete && !valid[uid] {
			if status == bookReading {
				log.Printf("not deleting book currently being read: %s", file)
			} else if err = os.Remove(file); err != nil {
				log.Printf("warning: failed to remove %s: %s", file, err)
			} else {
				log.Println("deleted", file)
				filesChanged.Store(true)
			}
		}
	}
}

func listOpenFds() []string {
	fds, _ := filepath.Glob("/proc/self/fd/*")
	var result []string
	for _, fd := range fds {
		if link, err := os.Readlink(fd); err == nil {
			result = append(result, link)
		}
	}
	return result
}
