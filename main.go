package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
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

// validate checks that all required config fields are present and sane.
func (c *appConfig) validate() error {
	if c.Server.URL == "" {
		return fmt.Errorf("Server.URL is required")
	}
	serverURL, err := url.Parse(c.Server.URL)
	if err != nil || (serverURL.Scheme != "http" && serverURL.Scheme != "https") || serverURL.Host == "" || serverURL.RawQuery != "" || serverURL.Fragment != "" {
		return fmt.Errorf("Server.URL must be a valid http or https URL with a host and no query or fragment")
	}
	if c.Server.Token == "" {
		return fmt.Errorf("Server.Token is required")
	}
	if c.Server.Timeout <= 0 {
		return fmt.Errorf("Server.Timeout must be greater than 0")
	}
	if c.Fetch.Workers <= 0 {
		return fmt.Errorf("Fetch.Workers must be greater than 0")
	}
	if c.Fetch.Workers > 32 {
		return fmt.Errorf("Fetch.Workers must not exceed 32")
	}
	if c.Fetch.Limit < 0 {
		return fmt.Errorf("Fetch.Limit must not be negative")
	}
	for _, status := range strings.Split(c.Fetch.Status, ",") {
		status = strings.TrimSpace(status)
		if status != "" && status != "unread" && status != "reading" && status != "read" {
			return fmt.Errorf("Fetch.Status contains invalid value %q", status)
		}
	}
	if c.Log.Size < 0 {
		return fmt.Errorf("Log.Size must not be negative")
	}
	if c.Output.Path == "" {
		return fmt.Errorf("Output.Path is required")
	}
	cleanOutputPath := filepath.Clean(c.Output.Path)
	if !filepath.IsAbs(c.Output.Path) || cleanOutputPath == filepath.VolumeName(cleanOutputPath)+string(filepath.Separator) {
		return fmt.Errorf("Output.Path must be an absolute path other than the filesystem root")
	}
	return nil
}

var buildVersion = "dev"

type app struct {
	cfg              appConfig
	readeck          readeckClient
	nickel           nickelLibrary
	lockFilePath     string
	nickelStatusPath string
}

func newApp(cfg appConfig) app {
	client := &http.Client{
		Timeout: time.Duration(cfg.Server.Timeout) * time.Second,
	}
	readeck := newReadeckClient(client, cfg.Server, cfg.Log.Verbose)
	return app{
		cfg:              cfg,
		readeck:          readeck,
		nickel:           nickelDatabase{path: defaultNickelDBPath, verbose: cfg.Log.Verbose},
		lockFilePath:     defaultLockFilePath,
		nickelStatusPath: defaultNickelStatusPath,
	}
}

func main() {
	flag.Parse()
	if err := os.MkdirAll(filepath.Dir(confPath), 0o755); err != nil {
		log.Fatal("create config directory: ", err)
	}
	configFile, cfg, configErr := findConfig()
	setupLogging(cfg, configFile)
	log.SetPrefix(fmt.Sprintf("pid=%d ", os.Getpid()))
	debug.SetPanicOnFault(true)

	if errors.Is(configErr, errConfigCreated) {
		log.Printf("no config found — template written to %s, please edit it", confPath)
		return
	} else if errors.Is(configErr, errUninstallRequested) {
		log.Println("empty config found — uninstalling")
		doUninstall(os.Args[0], installFiles)
		if err := os.RemoveAll(filepath.Dir(confPath)); err != nil {
			log.Fatal("remove application directory: ", err)
		}
		log.Println("uninstall complete")
		return
	} else if configErr != nil {
		log.Fatal("invalid configuration: ", configErr)
	}
	if err := cfg.validate(); err != nil {
		log.Fatal("invalid configuration: ", err)
	}
	log.Printf("kobodeck version %s loaded configuration from %s action=%q interface=%q",
		buildVersion, configFile, os.Getenv("ACTION"), os.Getenv("INTERFACE"))

	application := newApp(cfg)
	if *checkFlag {
		if err := application.runCheck(os.Stdout); err != nil {
			log.Fatal("check failed: ", err)
		}
		return
	}

	start := time.Now()
	defer func() {
		log.Printf("completed in %s", time.Since(start).Truncate(time.Millisecond))
	}()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	if err := application.sync(sigc); err != nil {
		log.Fatal(err)
	}
}

func debugf(verbose bool, format string, args ...interface{}) {
	if verbose {
		log.Printf(format, args...)
	}
}

const confPath = "/mnt/onboard/.adds/kobodeck/kobodeck.toml"

const (
	defaultNickelDBPath     = "/mnt/onboard/.kobo/KoboReader.sqlite"
	defaultNickelStatusPath = "/tmp/nickel-hardware-status"
	defaultLockFilePath     = "/tmp/kobodeck.lock"
)

var logPath = filepath.Join(filepath.Dir(confPath), "kobodeck.log")

// setupLogging configures the global logger to write to a size-capped rotating
// log file beside the resolved configuration file.
func setupLogging(cfg appConfig, configFilename string) {
	maxSizeMB := cfg.Log.Size
	if maxSizeMB < 1 {
		maxSizeMB = 1
	}
	filename := logPath
	if configFilename != "" {
		filename = filepath.Join(filepath.Dir(configFilename), "kobodeck.log")
	}
	log.SetOutput(&lumberjack.Logger{
		Filename:   filename,
		MaxSize:    maxSizeMB,
		MaxBackups: 7,
		MaxAge:     7,
	})
}

var errUninstallRequested = errors.New("uninstall requested")

// loadConfig decodes the TOML file at path.
// Returns os.ErrNotExist if the file is absent, errUninstallRequested if
// the file is empty, or an error for parse failures and unrecognised keys.
func loadConfig(path string) (_ appConfig, returnErr error) {
	f, err := os.Open(path)
	if err != nil {
		return appConfig{}, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, f.Close())
	}()
	info, err := f.Stat()
	if err != nil {
		return appConfig{}, err
	}
	if info.Size() == 0 {
		return appConfig{}, errUninstallRequested
	}
	var cfg appConfig
	md, err := toml.NewDecoder(f).Decode(&cfg)
	if err != nil {
		return appConfig{}, err
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return appConfig{}, fmt.Errorf("unknown keys: %v", keys)
	}
	return cfg, nil
}

// findConfig resolves the config path (--config flag or default) and loads it.
// For the default path only: if no config exists, a template is written there
// and the function returns errConfigCreated. If the config is empty,
// errUninstallRequested is returned.
func findConfig() (string, appConfig, error) {
	if *configFileFlag != "" {
		cfg, err := loadConfig(*configFileFlag)
		if err != nil {
			return "", appConfig{}, fmt.Errorf("load config %s: %w", *configFileFlag, err)
		}
		return *configFileFlag, cfg, nil
	}
	if _, err := os.Stat(confPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(confPath, configTemplate, 0o600); err != nil {
			return "", appConfig{}, fmt.Errorf("write config template: %w", err)
		}
		return confPath, appConfig{}, errConfigCreated
	}
	cfg, err := loadConfig(confPath)
	if err != nil {
		return "", appConfig{}, fmt.Errorf("load config %s: %w", confPath, err)
	}
	return confPath, cfg, nil
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
func nickelRescan(statusPath string) error {
	log.Println("triggering Nickel rescan")
	if err := appendNickelEvent(statusPath, "add"); err != nil {
		return err
	}
	time.Sleep(10 * time.Second)
	return appendNickelEvent(statusPath, "remove")
}

func appendNickelEvent(path, event string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("%s event: open %s: %w", event, path, err)
	}
	if _, err := f.WriteString("usb plug " + event + "\n"); err != nil {
		return errors.Join(fmt.Errorf("%s event: write %s: %w", event, path, err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%s event: close %s: %w", event, path, err)
	}
	return nil
}

// acquireLock acquires an exclusive non-blocking flock on path.
// Returns an error if another instance is already running.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("already running"), f.Close())
	}
	return f, nil
}

// runCheck prints the active configuration and lists bookmarks that would be
// synced, without downloading anything. Used by the --check flag.
func (a app) runCheck(w io.Writer) error {
	if _, err := io.WriteString(w, formatCheckConfig(a.cfg)+"Connecting to Readeck... "); err != nil {
		return fmt.Errorf("write check output: %w", err)
	}
	entries, err := a.readeck.listBookmarks(a.cfg.Fetch)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "OK\n\n"+formatCheckEntries(a.cfg, entries)); err != nil {
		return fmt.Errorf("write check output: %w", err)
	}
	return nil
}

func writeCheckOutput(w io.Writer, cfg appConfig, entries []readeckBookmark) error {
	_, err := io.WriteString(w, formatCheckConfig(cfg)+"Connecting to Readeck... OK\n\n"+formatCheckEntries(cfg, entries))
	return err
}

func formatCheckConfig(cfg appConfig) string {
	output := fmt.Sprintf(`Configuration:
  URL:     %s
  Output:  %s
  Workers: %d
  Limit:   %d
  Delete:  %v
`, cfg.Server.URL, cfg.Output.Path, cfg.Fetch.Workers, cfg.Fetch.Limit, cfg.Output.Delete)
	if cfg.Fetch.Labels != "" {
		output += fmt.Sprintf("  Labels:  %s\n\n", cfg.Fetch.Labels)
	} else {
		output += "  Labels:  (all)\n\n"
	}
	return output
}

func formatCheckEntries(cfg appConfig, entries []readeckBookmark) string {
	labelFilter := make(map[string]bool)
	if cfg.Fetch.Labels != "" {
		for _, l := range strings.Split(strings.ToLower(cfg.Fetch.Labels), ",") {
			labelFilter[strings.TrimSpace(l)] = true
		}
	}

	var matched, skipped int
	var output string
	for _, entry := range entries {
		if len(labelFilter) > 0 && !matchesLabelFilter(labelFilter, entry.Labels) {
			skipped++
			continue
		}
		matched++
		output += fmt.Sprintf("  %s — %s\n", entry.ID, entry.Title)
	}
	output += fmt.Sprintf("\n%d bookmarks to sync", matched)
	if skipped > 0 {
		output += fmt.Sprintf(", %d skipped (label filter)", skipped)
	}
	return output + "\n"
}
