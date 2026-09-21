package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var logTimestampPattern = regexp.MustCompile(`-[0-9]{8}-[0-9]{6}\.[0-9]{9}-p[0-9]+\.log$`)

// setupLogging creates a new log file for this run beside the resolved config.
func setupLogging(configFilename string) (*os.File, error) {
	configBase := filepath.Base(configFilename)
	configStem := strings.TrimSuffix(configBase, filepath.Ext(configBase))
	logDir := filepath.Dir(configFilename)
	logName := fmt.Sprintf("%s-%s-p%d.log", configStem, time.Now().UTC().Format("20060102-150405.000000000"), os.Getpid())
	logPath := filepath.Join(logDir, logName)
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create log file %s: %w", logPath, err)
	}
	log.SetOutput(file)
	return file, nil
}

// removeOldLogs retains the newest run logs matching currentLogPath.
func removeOldLogs(currentLogPath string, maxFiles int) error {
	if maxFiles < 1 {
		return fmt.Errorf("maximum log files must be positive")
	}
	dir := filepath.Dir(currentLogPath)
	current := filepath.Base(currentLogPath)
	suffix := logTimestampPattern.FindString(current)
	if suffix == "" {
		return fmt.Errorf("invalid run log filename %s", current)
	}
	prefix := strings.TrimSuffix(current, suffix)
	if prefix == "" {
		return fmt.Errorf("invalid run log filename %s", current)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read log directory %s: %w", dir, err)
	}
	kept := 0
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !logTimestampPattern.MatchString(name) {
			continue
		}
		if kept < maxFiles {
			kept++
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return nil
}

func debugf(verbose bool, format string, args ...interface{}) {
	if verbose {
		log.Printf(format, args...)
	}
}
