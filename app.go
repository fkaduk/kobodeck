package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

type app struct {
	cfg          appConfig
	readeck      readeckClient
	nickel       nickelLibrary
	lockFilePath string
	// TODO: explain why this is necessary
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

// run coordinates process startup and selects the check or sync workflow.
func run(output io.Writer, binaryPath string, signals <-chan os.Signal) error {
	if err := os.MkdirAll(filepath.Dir(confPath), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	configFile, cfg, configErr := findConfig()
	setupLogging(cfg, configFile)
	log.SetPrefix(fmt.Sprintf("pid=%d ", os.Getpid()))
	// TODO: EXPLAIN what this does
	debug.SetPanicOnFault(true)

	switch {
	case errors.Is(configErr, errConfigCreated):
		log.Printf("no config found — template written to %s, please edit it", confPath)
		return nil
	case errors.Is(configErr, errUninstallRequested):
		log.Println("empty config found — uninstalling")
		return uninstallApplication(binaryPath)
	case configErr != nil:
		return fmt.Errorf("invalid configuration: %w", configErr)
	}
	if err := cfg.validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	log.Printf("kobodeck version %s loaded configuration from %s action=%q interface=%q",
		buildVersion, configFile, os.Getenv("ACTION"), os.Getenv("INTERFACE"))

	application := newApp(cfg)
	if *checkFlag {
		if err := application.runCheck(output); err != nil {
			return fmt.Errorf("check failed: %w", err)
		}
		return nil
	}

	start := time.Now()
	defer func() {
		log.Printf("completed in %s", time.Since(start).Truncate(time.Millisecond))
	}()

	return application.sync(signals)
}
