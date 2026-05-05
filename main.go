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
	"golang.org/x/sync/errgroup"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	configFileFlag = flag.String("config", "", "path to the configuration file")
	checkFlag      = flag.Bool("check", false, "validate config and show what would be synced, then exit")
)

type appConfig struct {
	URL     string `toml:"URL"`
	Token   string `toml:"Token"`
	Verbose bool   `toml:"Verbose"`
	Delete  bool   `toml:"Delete"`
	Log     string `toml:"Log"`
	Workers int    `toml:"Workers"`
	Limit   int    `toml:"Limit"`
	Output  string `toml:"Output"`
	Timeout int    `toml:"Timeout"`
	Labels  string `toml:"Labels"`
	Kepub   bool   `toml:"Kepub"`
	Covers  bool   `toml:"Covers"`
}

var config appConfig

// validate checks that all required config fields are present and sane.
func (c *appConfig) validate() error {
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
	version      = "dev"
	nickelDBPath = "/mnt/onboard/.kobo/KoboReader.sqlite"
)

func main() {
	flag.Parse()
	configFile, configErr := findConfig()
	debugf("config: %#v", config)

	setupLogging(config)
	debug.SetPanicOnFault(true)

	if configErr != nil {
		setupLogging(appConfig{Log: "/mnt/onboard/.kobodeck.log"})
		log.Println("no config found at", confPath, "— uninstalling")
		uninstall()
	}
	if err := config.validate(); err != nil {
		log.Fatal("invalid configuration: ", err)
	}
	log.Println("kobodeck version", version, "loaded configuration from", configFile)

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

	lock, err := acquireLock()
	if err != nil {
		log.Fatal(err)
	}
	defer lock.Close()

	log.Println("connecting to", config.URL)
	client := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	entries, err := listBookmarks()
	for attempt := 1; err != nil && attempt < 5; attempt++ {
		delay := time.Duration(1<<uint(attempt)) * time.Second
		log.Printf("failed to connect, retrying in %s: %v", delay, err)
		time.Sleep(delay)
		entries, err = listBookmarks()
	}
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

	var g errgroup.Group
	g.SetLimit(config.Workers)

	for _, entry := range entries {
		if len(tags) > 0 && !matchesLabelFilter(tags, entry.Labels) {
			debugf("skipping %s (not in tags)", entry.ID)
			continue
		}
		select {
		case sig := <-sigc:
			log.Println("got signal:", sig, ", waiting for downloads to finish...")
			goto done
		default:
		}
		debugf("dispatching %s", entry.ID)
		valid[entry.ID] = true
		g.Go(func() error {
			return download(client, entry)
		})
	}
done:
	if err := g.Wait(); err != nil {
		log.Println("download error:", err)
	}

	reconcileLocalFiles(config, valid)

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

func debugf(format string, args ...interface{}) {
	if config.Verbose {
		log.Printf(format, args...)
	}
}

// setupLogging configures the global logger to write to stdout and optionally
// to a size-capped rotating log file when cfg.Log is set.
func setupLogging(cfg appConfig, extraWriters ...io.Writer) {
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

const confPath = "/mnt/onboard/.kobodeck.toml"

// findConfig loads the config file, using --config if provided, otherwise the
// default path on the Kobo's onboard storage.
func findConfig() (string, error) {
	path := confPath
	if *configFileFlag != "" {
		path = *configFileFlag
	}
	_, err := toml.DecodeFile(path, &config)
	if err != nil {
		return "", fmt.Errorf("load config %s: %w", path, err)
	}
	return path, nil
}

// uninstall removes all files deployed by KoboRoot.tgz and exits.
// Refuses to run if the binary is not under /usr/local to prevent accidents.
func uninstall() {
	log.Println("uninstall requested, clearing myself out")
	if !strings.HasPrefix(os.Args[0], "/usr/local") {
		log.Fatal("unexpected command path, aborting uninstall:", os.Args[0])
	}
	files := []string{
		"/etc/udev/rules.d/90-kobodeck.rules",
		"/usr/local/bin/fake-connect-usb",
		"/usr/local/bin/kobodeck-run",
		"/usr/local/bin/kobodeck",
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

// acquireLock acquires an exclusive non-blocking flock on /tmp/kobodeck.lock.
// Returns an error if another instance is already running.
func acquireLock() (*os.File, error) {
	f, err := os.OpenFile("/tmp/kobodeck.lock", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("already running")
	}
	return f, nil
}

// runCheck prints the active configuration and lists bookmarks that would be
// synced, without downloading anything. Used by the --check flag.
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
	entries, err := listBookmarks()
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
		if len(labelFilter) > 0 && !matchesLabelFilter(labelFilter, entry.Labels) {
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

// reconcileLocalFiles checks each local EPUB against the Nickel DB and the valid
// set. Books marked as read in Nickel are archived in Readeck. Books no longer
// in the unread feed are deleted if cfg.Delete is set, unless currently being read.
func reconcileLocalFiles(cfg appConfig, valid map[string]bool) {
	outputDir := strings.TrimSuffix(cfg.Output, "/")
	files, _ := filepath.Glob(outputDir + "/*.epub")
	debugf("local files to inspect: %v", files)
	for _, file := range files {
		uid := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(file), ".epub"), ".kepub")
		if uid == "" {
			log.Println("skipping file with empty name:", file)
			continue
		}
		status, err := nickelReadStatus(uid, outputDir)
		if err != nil {
			// Skip entirely — don't delete a book we can't confirm the read state of.
			log.Println(err)
			continue
		}
		if status == bookRead {
			if err = archiveBookmark(uid); err != nil {
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

// listOpenFds returns the resolved paths of all open file descriptors.
// Used for verbose leak diagnostics only.
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
