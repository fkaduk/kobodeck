package main

import (
	"log"
	"path/filepath"

	// TODO: is this really the best options? why not use slog?
	"gopkg.in/natefinch/lumberjack.v2"
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

func debugf(verbose bool, format string, args ...interface{}) {
	if verbose {
		log.Printf(format, args...)
	}
}
