package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"syscall"
	"time"
)

var (
	configFileFlag = flag.String("config", "", "path to the configuration file")
	checkFlag      = flag.Bool("check", false, "validate config and show what would be synced, then exit")
	// release builds replace this with -ldflags=-X.
	buildVersion = "dev"
)

const (
	defaultConfigPath = "/mnt/onboard/.adds/kobodeck/kobodeck.toml"
)

type app struct {
	cfg              appConfig
	readeck          readeckClient
	nickel           nickelLibrary
	nickelStatusPath string
}

func main() {
	flag.Parse()

	configFile := defaultConfigPath
	if *configFileFlag != "" {
		configFile = *configFileFlag
	}
	logFile, err := setupLogging(configFile)
	if err != nil {
		log.Fatal(err)
	}
	defer closeWithWarning("log file", logFile)
	lock, err := acquireLock(defaultLockFilePath)
	if err != nil {
		log.Fatal(err)
	}
	defer closeWithWarning("lock file", lock)
	log.Printf("loading config file %s", configFile)
	cfg, configErr := loadConfig(configFile)
	if errors.Is(configErr, os.ErrNotExist) && configFile == defaultConfigPath {
		log.Println("default config not found — uninstalling")
		if err := uninstallApplication(os.Args[0]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if configErr != nil {
		log.Fatal(fmt.Errorf("invalid configuration: %w", configErr))
	}
	if err := cfg.validate(); err != nil {
		log.Fatal(fmt.Errorf("invalid configuration: %w", err))
	}
	if err := removeOldLogs(logFile.Name(), cfg.Log.RetainedFiles); err != nil {
		log.Fatal(fmt.Errorf("remove old log files: %w", err))
	}
	log.Printf(
		"kobodeck version %s loaded configuration from %s action=%q interface=%q",
		buildVersion, configFile, os.Getenv("ACTION"), os.Getenv("INTERFACE"))

	application := newApp(cfg)
	// TODO: can we just pass this flag down to sync and adjust this inline ?
	if *checkFlag {
		err := application.runCheck(os.Stdout)
		if err != nil {
			log.Fatal(fmt.Errorf("check failed: %w", err))
		}
		return
	}

	start := time.Now()
	defer func() {
		log.Printf("completed in %s", time.Since(start).Truncate(time.Millisecond))
	}()
	err = application.sync()
	if err != nil {
		log.Fatal(err)
	}
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
		nickelStatusPath: defaultNickelStatusPath,
	}
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
