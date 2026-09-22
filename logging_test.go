package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupLoggingUsesConfigDirectory(t *testing.T) {
	// Given
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "custom.toml")
	// When
	logFile, err := setupLogging(configPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeWithWarning("test log file", logFile)
		log.SetOutput(io.Discard)
	}()
	log.Print("custom config logging test")
	// Then
	entries, err := os.ReadDir(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".log") {
		t.Fatalf("unexpected log files: %v", entries)
	}
}

func TestRemoveOldLogsRetainsMostRecentRunLogs(t *testing.T) {
	// Given
	dir := t.TempDir()
	const maxFiles = 10
	for i := 0; i < maxFiles+2; i++ {
		name := fmt.Sprintf("custom-20260101-0000%02d.000000000-p%d.log", i, i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	current := fmt.Sprintf("custom-20260102-000000.000000000-p%d.log", os.Getpid())
	if err := os.WriteFile(filepath.Join(dir, current), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// When
	if err := removeOldLogs(filepath.Join(dir, current), maxFiles); err != nil {
		t.Fatal(err)
	}
	// Then
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxFiles {
		t.Fatalf("retained %d entries, want %d", len(entries), maxFiles)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".log") {
			t.Fatalf("unexpected retained file: %s", entry.Name())
		}
	}
}

func TestRemoveOldLogsRejectsInvalidFileLimit(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "custom-current.log")
	// When / Then
	if err := removeOldLogs(path, 0); err == nil {
		t.Fatal("removeOldLogs accepted a non-positive file limit")
	}
}
