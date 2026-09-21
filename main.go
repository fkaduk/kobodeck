package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
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
	if *configFileFlag != "" {
		configFile = *configFileFlag
	}
	logFile, err := setupLogging(configFile, retainedLines)
	if err != nil {
		log.Fatal(err)
	}
	defer closeWithWarning("log file", logFile)
	log.SetPrefix(fmt.Sprintf("pid=%d ", os.Getpid()))

	log.Printf("loading config file %s", configFile)
	cfg, configErr := loadConfig(configFile)
	if errors.Is(configErr, errUninstallRequested) {
		log.Println("empty config found — uninstalling")
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
	log.Printf(
		"kobodeck version %s loaded configuration from %s action=%q interface=%q",
		buildVersion, configFile, os.Getenv("ACTION"), os.Getenv("INTERFACE"))

	application := newApp(cfg)
	// TODO: can we just pass this flag down to sync and adjust this inline ?
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
