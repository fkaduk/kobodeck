package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

var (
	configFileFlag = flag.String("config", "", "path to the configuration file")
	checkFlag      = flag.Bool("check", false, "validate config and show what would be synced, then exit")
	// release builds replace this with -ldflags=-X.
	buildVersion = "dev"
)

const defaultConfigPath = "/mnt/onboard/.adds/kobodeck/kobodeck.toml"

func main() {
	flag.Parse()

	configFile := defaultConfigPath
	createConfig := *configFileFlag == ""
	configMissing := false
	if !createConfig {
		configFile = *configFileFlag
	} else if _, err := os.Stat(configFile); errors.Is(err, os.ErrNotExist) {
		configMissing = true
		if err := os.MkdirAll(filepath.Dir(configFile), 0o755); err != nil {
			log.Fatal(fmt.Errorf("create config directory: %w", err))
		}
	}
	logFile, err := setupLogging(configFile)
	if err != nil {
		log.Fatal(err)
	}
	defer closeWithWarning("log file", logFile)
	log.SetPrefix(fmt.Sprintf("pid=%d ", os.Getpid()))
	if configMissing {
		if err := os.WriteFile(configFile, configTemplate, 0o600); err != nil {
			log.Fatal(fmt.Errorf("write config template: %w", err))
		}
		log.Printf("no config found — template written to %s, please edit it", configFile)
		return
	}
	cfg, configErr := loadConfig(configFile)

	switch {
	case errors.Is(configErr, errUninstallRequested):
		log.Println("empty config found — uninstalling")
		if err := uninstallApplication(os.Args[0]); err != nil {
			log.Fatal(err)
		}
		return
	case configErr != nil:
		log.Fatal(fmt.Errorf("invalid configuration: load config %s: %w", configFile, configErr))
	}
	if err := cfg.validate(); err != nil {
		log.Fatal(fmt.Errorf("invalid configuration: %w", err))
	}
	log.Printf("kobodeck version %s loaded configuration from %s action=%q interface=%q",
		buildVersion, configFile, os.Getenv("ACTION"), os.Getenv("INTERFACE"))

	application := newApp(cfg)
	if *checkFlag {
		if err := application.runCheck(os.Stdout); err != nil {
			log.Fatal(fmt.Errorf("check failed: %w", err))
		}
		return
	}

	start := time.Now()
	defer func() {
		log.Printf("completed in %s", time.Since(start).Truncate(time.Millisecond))
	}()
	if err := application.sync(); err != nil {
		log.Fatal(err)
	}
}
