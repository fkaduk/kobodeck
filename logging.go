package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const retainedLines = 20_000

type logFile struct {
	file *os.File
	path string
}

// setupLogging configures the global logger to write to a bounded log file
// beside the resolved configuration file.
func setupLogging(configFilename string, maxLines int) (*logFile, error) {
	configBase := filepath.Base(configFilename)
	filename := strings.TrimSuffix(configBase, filepath.Ext(configBase)) + ".log"
	filename = filepath.Join(filepath.Dir(configFilename), filename)
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", filename, err)
	}
	logger := &logFile{file: file, path: filename}
	if err := logger.trimIfNeeded(maxLines); err != nil {
		return nil, errors.Join(fmt.Errorf("trim log file %s: %w", filename, err), file.Close())
	}
	log.SetOutput(logger)
	return logger, nil
}

func (logger *logFile) Write(p []byte) (int, error) {
	return logger.file.Write(p)
}

func (logger *logFile) Close() error {
	return logger.file.Close()
}

// trimIfNeeded keeps only the newest 20,000 lines in the existing log.
func (logger *logFile) trimIfNeeded(maxLines int) error {
	data, err := os.ReadFile(logger.path)
	if err != nil {
		return fmt.Errorf("read log file %s: %w", logger.path, err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	lineCount := len(lines)
	if len(data) > 0 && data[len(data)-1] == '\n' {
		lineCount--
	}
	if lineCount <= maxLines {
		return nil
	}
	data = bytes.Join(lines[lineCount-maxLines:], []byte{'\n'})
	if err := logger.file.Truncate(0); err != nil {
		return fmt.Errorf("truncate log file %s: %w", logger.path, err)
	}
	if _, err := logger.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek log file %s: %w", logger.path, err)
	}
	if _, err := logger.file.Write(data); err != nil {
		return errors.Join(fmt.Errorf("rewrite log file %s: %w", logger.path, err), logger.file.Sync())
	}
	return nil
}

func debugf(verbose bool, format string, args ...interface{}) {
	if verbose {
		log.Printf(format, args...)
	}
}
