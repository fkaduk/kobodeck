package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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

//go:embed kobodeck.toml
var configTemplate []byte

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
	Verbose bool `toml:"Verbose"`
	Size    int  `toml:"Size"` // in MB
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
	os.MkdirAll(filepath.Dir(confPath), 0755)
	configFile, configErr := findConfig()
	setupLogging(config)
	debug.SetPanicOnFault(true)

	if errors.Is(configErr, errConfigCreated) {
		log.Printf("no config found — template written to %s, please edit it", confPath)
		return
	} else if errors.Is(configErr, errUninstallRequested) {
		log.Println("empty config found — uninstalling")
		doUninstall(os.Args[0], installFiles)
		os.RemoveAll(filepath.Dir(confPath))
		log.Println("uninstall complete")
		return
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
	if filesChanged.Load() {
		nickelRescan()
	}
}

func debugf(format string, args ...interface{}) {
	if config.Log.Verbose {
		log.Printf(format, args...)
	}
}

const confPath = "/mnt/onboard/.adds/kobodeck/kobodeck.toml"

var logPath = filepath.Join(filepath.Dir(confPath), "kobodeck.log")

// setupLogging configures the global logger to write to a size-capped rotating
// log file at the hardcoded path.
func setupLogging(cfg appConfig) {
	maxSizeMB := cfg.Log.Size
	if maxSizeMB < 1 {
		maxSizeMB = 1
	}
	log.SetOutput(&lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    maxSizeMB,
		MaxBackups: 7,
		MaxAge:     7,
	})
}

var errUninstallRequested = errors.New("uninstall requested")

// loadConfig decodes the TOML file at path into the global config.
// Returns os.ErrNotExist if the file is absent, errUninstallRequested if
// the file is empty, or an error for parse failures and unrecognised keys.
func loadConfig(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return errUninstallRequested
	}
	md, err := toml.NewDecoder(f).Decode(&config)
	if err != nil {
		return err
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return fmt.Errorf("unknown keys: %v", keys)
	}
	return nil
}

// findConfig resolves the config path (--config flag or default) and loads it.
// For the default path only: if no config exists, a template is written there
// and the function returns errConfigCreated. If the config is empty,
// errUninstallRequested is returned.
func findConfig() (string, error) {
	if *configFileFlag != "" {
		if err := loadConfig(*configFileFlag); err != nil {
			return "", fmt.Errorf("load config %s: %w", *configFileFlag, err)
		}
		return *configFileFlag, nil
	}
	if _, err := os.Stat(confPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(confPath, configTemplate, 0600); err != nil {
			return "", fmt.Errorf("write config template: %w", err)
		}
		return confPath, errConfigCreated
	}
	if err := loadConfig(confPath); err != nil {
		return "", fmt.Errorf("load config %s: %w", confPath, err)
	}
	return confPath, nil
}

var errConfigCreated = errors.New("config template created")

var installFiles = []string{
	"/etc/udev/rules.d/90-kobodeck.rules",
	"/usr/local/bin/kobodeck",
}

// doUninstall removes the given files and logs the result.
// Refuses to run if binaryPath is not under /usr/local to prevent accidents.
func doUninstall(binaryPath string, files []string) {
	log.Println("uninstall requested, clearing myself out")
	if !strings.HasPrefix(binaryPath, "/usr/local") {
		log.Fatal("unexpected command path, aborting uninstall:", binaryPath)
	}
	var lastErr error
	for _, file := range files {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("failed to remove %s: %s", file, err)
			lastErr = err
		} else {
			log.Printf("deleted %s", file)
		}
	}
	if lastErr != nil {
		log.Fatal("uninstall partially failed")
	}
}

// nickelRescan triggers a Nickel library rescan by simulating a USB plug/unplug
// via /tmp/nickel-hardware-status. The user will see a Connect/Cancel dialog;
// pressing Connect rescans immediately, Cancel still picks up changes on reboot.
func nickelRescan() {
	const nickelStatus = "/tmp/nickel-hardware-status"
	log.Println("triggering Nickel rescan")
	if f, err := os.OpenFile(nickelStatus, os.O_APPEND|os.O_WRONLY, 0); err == nil {
		f.WriteString("usb plug add\n")
		f.Close()
		time.Sleep(10 * time.Second)
		if f, err = os.OpenFile(nickelStatus, os.O_APPEND|os.O_WRONLY, 0); err == nil {
			f.WriteString("usb plug remove\n")
			f.Close()
		}
	}
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
		db, err := openNickelDB()
		if err != nil {
			log.Println("cannot open Nickel DB:", err)
			continue
		}
		status, statusErr := nickelReadStatus(db, uid, outputDir)
		var inCollection bool
		if cfg.Sync.FavouriteCollection != "" {
			inCollection, err = nickelIsInCollection(db, uid, outputDir, cfg.Sync.FavouriteCollection)
			if err != nil {
				log.Println("failed to check collection:", err)
			}
		}
		db.Close()
		if statusErr != nil {
			// Skip entirely — don't delete a book we can't confirm the read state of.
			log.Println(statusErr)
			continue
		}
		if cfg.Sync.Archive && status == bookRead && valid[uid] {
			log.Printf("marking entry %s as archived", uid)
			if err = patchBookmark(client, uid, map[string]bool{"is_archived": true}); err != nil {
				log.Println("failed to mark as read:", err)
			} else {
				valid[uid] = false
			}
		}
		if inCollection {
			log.Printf("marking entry %s as favourite", uid)
			if err = patchBookmark(client, uid, map[string]bool{"is_marked": true}); err != nil {
				log.Println("failed to mark as favourite:", err)
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
