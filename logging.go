package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const retainedLogFiles = 10

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
	name := strings.TrimSuffix(current, filepath.Ext(current))
	separator := strings.LastIndex(name, "-20")
	if separator < 0 {
		return fmt.Errorf("invalid run log filename %s", current)
	}
	prefix := name[:separator+1]
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read log directory %s: %w", dir, err)
	}
	var logs []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == current || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		logs = append(logs, entry.Name())
	}
	sort.Sort(sort.Reverse(sort.StringSlice(logs)))
	if len(logs) <= maxFiles-1 {
		return nil
	}
	for _, name := range logs[maxFiles-1:] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("remove %s: %w", filepath.Join(dir, name), err)
		}
	}
	return nil
}

func debugf(verbose bool, format string, args ...interface{}) {
	if verbose {
		log.Printf(format, args...)
	}
}
