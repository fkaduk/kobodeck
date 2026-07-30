package main

import (
	"database/sql"
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
	Status  string `toml:"Status"`
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
	os.MkdirAll(filepath.Dir(confPath), 0o755)
	configFile, configErr := findConfig()
	setupLogging(config)
	log.SetPrefix(fmt.Sprintf("pid=%d ", os.Getpid()))
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
	log.Printf("kobodeck version %s loaded configuration from %s action=%q interface=%q",
		version, configFile, os.Getenv("ACTION"), os.Getenv("INTERFACE"))

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
	bookmarks := make(map[string]readeckBookmark)
	tags := make(map[string]bool)
	if len(config.Fetch.Labels) > 0 {
		for _, tag := range strings.Split(strings.ToLower(config.Fetch.Labels), ",") {
			tags[strings.TrimSpace(tag)] = true
		}
	}

	var g errgroup.Group
	g.SetLimit(config.Fetch.Workers)

	for _, entry := range entries {
		bookmarks[entry.ID] = entry
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

	reconcileLocalFiles(client, config, valid, bookmarks)

	if filesChanged.Load() {
		if err := nickelRescan(); err != nil {
			log.Printf("Nickel rescan failed: %v", err)
		}
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
		if err := os.WriteFile(confPath, configTemplate, 0o600); err != nil {
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
func nickelRescan() error {
	const nickelStatus = "/tmp/nickel-hardware-status"
	log.Println("triggering Nickel rescan")
	if err := appendNickelEvent(nickelStatus, "add"); err != nil {
		return err
	}
	time.Sleep(10 * time.Second)
	return appendNickelEvent(nickelStatus, "remove")
}

func appendNickelEvent(path, event string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("%s event: open %s: %w", event, path, err)
	}
	if _, err := f.WriteString("usb plug " + event + "\n"); err != nil {
		f.Close()
		return fmt.Errorf("%s event: write %s: %w", event, path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%s event: close %s: %w", event, path, err)
	}
	return nil
}

// acquireLock acquires an exclusive non-blocking flock on /tmp/kobodeck.lock.
// Returns an error if another instance is already running.
func acquireLock() (*os.File, error) {
	f, err := os.OpenFile("/tmp/kobodeck.lock", os.O_CREATE|os.O_WRONLY, 0o600)
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

// reconcileLocalFiles checks each local EPUB against the Nickel DB and Readeck.
// Books marked as read in Nickel are marked as read in Readeck and optionally
// archived. When FavouriteCollection is configured, Readeck favourite state
// mirrors Kobo shelf membership, including for archived bookmarks. Books no
// longer in the fetched feed are deleted if cfg.Output.Delete is set, unless
// currently being read.
func reconcileLocalFiles(
	client *http.Client,
	cfg appConfig,
	valid map[string]bool,
	bookmarks map[string]readeckBookmark,
) {
	outputDir := strings.TrimSuffix(cfg.Output.Path, "/")
	files, _ := filepath.Glob(outputDir + "/*.epub")
	debugf("local files to inspect: %v", files)
	for _, file := range files {
		uid := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(file), ".epub"), ".kepub")
		if uid == "" {
			log.Println("skipping file with empty name:", file)
			continue
		}
		// Keep the feed membership from the beginning of this reconciliation.
		// Archiving below removes the entry from valid, but it must still be
		// favourited during this same run. On the next run an archived entry is
		// absent from the feed, so this also avoids sending the same favourite
		// PATCH on every Wi-Fi connection.
		wasValid := valid[uid]

		db, err := sql.Open("sqlite", "file:"+nickelDBPath+"?mode=ro")
		if err != nil {
			log.Println("cannot open Nickel DB:", err)
			continue
		}
		status, statusErr := nickelReadStatus(db, uid, outputDir)
		var inCollection bool
		collectionKnown := cfg.Sync.FavouriteCollection == ""
		if cfg.Sync.FavouriteCollection != "" {
			inCollection, err = nickelIsInCollection(db, uid, outputDir, cfg.Sync.FavouriteCollection)
			if err != nil {
				log.Println("failed to check collection:", err)
			} else {
				collectionKnown = true
			}
		}
		db.Close()
		if statusErr != nil {
			// Skip entirely — don't delete a book we can't confirm the read state of.
			log.Println(statusErr)
			continue
		}
		bookmark, bookmarkKnown := bookmarks[uid]
		if status == bookRead && wasValid {
			fields := make(map[string]any)
			action := "read"
			if bookmark.ReadProgress != 100 {
				fields["read_progress"] = 100
			}
			if cfg.Sync.Archive {
				fields["is_archived"] = true
				action += " and archived"
			}
			if len(fields) > 0 {
				log.Printf("marking entry %s as %s", uid, action)
				if err = patchBookmark(client, uid, fields); err != nil {
					log.Printf("failed to mark entry %s as %s: %v", uid, action, err)
				} else if cfg.Sync.Archive {
					valid[uid] = false
				}
			}
		}
		if collectionKnown && cfg.Sync.FavouriteCollection != "" && !bookmarkKnown {
			bookmark, err = getBookmark(client, uid)
			if err != nil {
				log.Printf("cannot read Readeck favourite state for %s: %v", uid, err)
			} else {
				bookmarkKnown = true
			}
		}
		if collectionKnown && bookmarkKnown && cfg.Sync.FavouriteCollection != "" &&
			inCollection != bookmark.IsMarked {
			action := "marking"
			if !inCollection {
				action = "unmarking"
			}
			log.Printf("%s entry %s as favourite", action, uid)
			if err = patchBookmark(client, uid, map[string]any{"is_marked": inCollection}); err != nil {
				log.Printf("failed to set favourite state to %t: %v", inCollection, err)
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
