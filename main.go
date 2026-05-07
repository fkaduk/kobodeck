package main

import (
	"errors"
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
	Server serverConfig `toml:"Server"`
	Fetch  fetchConfig  `toml:"Fetch"`
	Sync   syncConfig   `toml:"Sync"`
	Log    logConfig    `toml:"Log"`
	Output outputConfig `toml:"Output"`
}

type serverConfig struct {
	URL     string `toml:"URL"`
	Token   string `toml:"Token"`
	Timeout int    `toml:"Timeout"`
}

type fetchConfig struct {
	Workers int    `toml:"Workers"`
	Limit   int    `toml:"Limit"`
	Labels  string `toml:"Labels"`
}

type syncConfig struct {
	Archive             bool   `toml:"Archive"`
	FavouriteCollection string `toml:"FavouriteCollection"`
}

type logConfig struct {
	Path    string `toml:"Path"`
	Verbose bool   `toml:"Verbose"`
	Size    int    `toml:"Size"` // in MB
}

type outputConfig struct {
	Path   string `toml:"Path"`
	Delete bool   `toml:"Delete"`
}

var config appConfig

// validate checks that all required config fields are present and sane.
func (c *appConfig) validate() error {
	if c.Server.URL == "" {
		return fmt.Errorf("Server.URL is required")
	}
	if c.Server.Token == "" {
		return fmt.Errorf("Server.Token is required")
	}
	if c.Output.Path == "" {
		return fmt.Errorf("Output.Path is required")
	}
	if c.Fetch.Workers <= 0 {
		return fmt.Errorf("Fetch.Workers must be greater than 0")
	}
	if c.Server.Timeout <= 0 {
		return fmt.Errorf("Server.Timeout must be greater than 0")
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
	debugf("config: URL=%s workers=%d limit=%d labels=%q output=%s delete=%v archive=%v",
		config.Server.URL, config.Fetch.Workers, config.Fetch.Limit,
		config.Fetch.Labels, config.Output.Path, config.Output.Delete, config.Sync.Archive)

	setupLogging(config)
	debug.SetPanicOnFault(true)

	if errors.Is(configErr, os.ErrNotExist) {
		setupLogging(appConfig{Log: logConfig{Path: "/mnt/onboard/.kobodeck.log"}})
		log.Println("no config found at", confPath, "— uninstalling")
		uninstall()
	} else if configErr != nil {
		log.Fatal("invalid configuration: ", configErr)
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

	log.Println("connecting to", config.Server.URL)
	client := &http.Client{
		Timeout: time.Duration(config.Server.Timeout) * time.Second,
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	entries, err := listBookmarks(client)
	for attempt := 1; err != nil && attempt < 5; attempt++ {
		delay := time.Duration(1<<uint(attempt)) * time.Second
		log.Printf("failed to connect, retrying in %s: %v", delay, err)
		time.Sleep(delay)
		entries, err = listBookmarks(client)
	}
	if err != nil {
		log.Fatal(err)
	}

	valid := make(map[string]bool)
	tags := make(map[string]bool)
	if len(config.Fetch.Labels) > 0 {
		for _, tag := range strings.Split(strings.ToLower(config.Fetch.Labels), ",") {
			tags[strings.TrimSpace(tag)] = true
		}
	}

	var g errgroup.Group
	g.SetLimit(config.Fetch.Workers)

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

	reconcileLocalFiles(client, config, valid)

	if config.Log.Verbose {
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
	if config.Log.Verbose {
		log.Printf(format, args...)
	}
}

// setupLogging configures the global logger to write to stdout and optionally
// to a size-capped rotating log file when cfg.Log.Path is set.
func setupLogging(cfg appConfig, extraWriters ...io.Writer) {
	writers := []io.Writer{os.Stdout}
	writers = append(writers, extraWriters...)
	if len(cfg.Log.Path) > 0 {
		maxSizeMB := cfg.Log.Size
		if maxSizeMB < 1 {
			maxSizeMB = 1
		}
		writers = append(writers, &lumberjack.Logger{
			Filename:   cfg.Log.Path,
			MaxSize:    maxSizeMB,
			MaxBackups: 7,
			MaxAge:     7,
		})
	}
	log.SetOutput(io.MultiWriter(writers...))
}

const confPath = "/mnt/onboard/.kobodeck.toml"

// loadConfig decodes the TOML file at path into the global config.
// Returns os.ErrNotExist if the file is absent, or an error for parse
// failures and unrecognised keys.
func loadConfig(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	md, err := toml.NewDecoder(f).Decode(&config)
	if err != nil {
		return err
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return fmt.Errorf("unknown keys: %v", keys)
	}
	return nil
}

// findConfig resolves the config path (--config flag or default), loads it,
// and wraps any error with the path for context.
func findConfig() (string, error) {
	path := confPath
	if *configFileFlag != "" {
		path = *configFileFlag
	}
	if err := loadConfig(path); err != nil {
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
	fmt.Fprintf(w, "  URL:     %s\n", config.Server.URL)
	fmt.Fprintf(w, "  Output:  %s\n", config.Output.Path)
	fmt.Fprintf(w, "  Workers: %d\n", config.Fetch.Workers)
	fmt.Fprintf(w, "  Limit:   %d\n", config.Fetch.Limit)
	fmt.Fprintf(w, "  Delete:  %v\n", config.Output.Delete)
	if config.Fetch.Labels != "" {
		fmt.Fprintf(w, "  Labels:  %s\n", config.Fetch.Labels)
	} else {
		fmt.Fprintln(w, "  Labels:  (all)")
	}
	fmt.Fprintln(w)

	fmt.Fprint(w, "Connecting to Readeck... ")
	client := &http.Client{Timeout: time.Duration(config.Server.Timeout) * time.Second}
	entries, err := listBookmarks(client)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "OK")
	fmt.Fprintln(w)

	labelFilter := make(map[string]bool)
	if config.Fetch.Labels != "" {
		for _, l := range strings.Split(strings.ToLower(config.Fetch.Labels), ",") {
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
// set. Books marked as read in Nickel are archived in Readeck. Books in the
// configured FavouriteCollection shelf are marked as favourite. Books no longer
// in the unread feed are deleted if cfg.Output.Delete is set, unless currently
// being read.
func reconcileLocalFiles(client *http.Client, cfg appConfig, valid map[string]bool) {
	outputDir := strings.TrimSuffix(cfg.Output.Path, "/")
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
		if cfg.Sync.Archive && status == bookRead {
			log.Printf("marking entry %s as archived", uid)
			if err = patchBookmark(client, uid, map[string]bool{"is_archived": true}); err != nil {
				log.Println("failed to mark as read:", err)
			} else {
				valid[uid] = false
			}
		}
		if cfg.Sync.FavouriteCollection != "" {
			inCollection, err := nickelIsInCollection(uid, outputDir, cfg.Sync.FavouriteCollection)
			if err != nil {
				log.Println("failed to check collection:", err)
			} else if inCollection {
				log.Printf("marking entry %s as favourite", uid)
				if err = patchBookmark(client, uid, map[string]bool{"is_marked": true}); err != nil {
					log.Println("failed to mark as favourite:", err)
				}
			}
		}
		if cfg.Output.Delete && !valid[uid] {
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
