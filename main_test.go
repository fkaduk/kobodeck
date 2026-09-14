package main

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newValidAppConfig(outputPath string) appConfig {
	return appConfig{
		Server: serverConfig{URL: "https://readeck.example/api", Token: "token", Timeout: 5},
		Fetch:  fetchConfig{Workers: 2, Limit: 10, Status: "unread,reading"},
		Log:    logConfig{Size: 1},
		Output: outputConfig{Path: outputPath},
	}
}

func TestAppConfigValidation(t *testing.T) {
	// Given
	outputPath := t.TempDir()
	tests := []struct {
		name   string
		mutate func(*appConfig)
		valid  bool
	}{
		{name: "http URL", mutate: func(c *appConfig) { c.Server.URL = "http://localhost:8080/readeck/" }, valid: true},
		{name: "URL with query", mutate: func(c *appConfig) { c.Server.URL = "https://readeck.example?token=x" }},
		{name: "URL with fragment", mutate: func(c *appConfig) { c.Server.URL = "https://readeck.example/#api" }},
		{name: "URL without host", mutate: func(c *appConfig) { c.Server.URL = "https:///api" }},
		{name: "URL with unsupported scheme", mutate: func(c *appConfig) { c.Server.URL = "ftp://readeck.example" }},
		{name: "too many workers", mutate: func(c *appConfig) { c.Fetch.Workers = 33 }},
		{name: "negative limit", mutate: func(c *appConfig) { c.Fetch.Limit = -1 }},
		{name: "empty status", mutate: func(c *appConfig) { c.Fetch.Status = "" }, valid: true},
		{name: "valid statuses", mutate: func(c *appConfig) { c.Fetch.Status = "unread, reading,read" }, valid: true},
		{name: "invalid status", mutate: func(c *appConfig) { c.Fetch.Status = "unread,finished" }},
		{name: "negative log size", mutate: func(c *appConfig) { c.Log.Size = -1 }},
		{name: "relative output", mutate: func(c *appConfig) { c.Output.Path = "books" }},
		{name: "root output", mutate: func(c *appConfig) { c.Output.Path = "/" }},
		{name: "absolute output", mutate: func(c *appConfig) { c.Output.Path = outputPath }, valid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidAppConfig(outputPath)
			tt.mutate(&cfg)
			// When
			err := cfg.validate()
			// Then
			if tt.valid && err != nil {
				t.Fatalf("validate() returned unexpected error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("validate() accepted invalid configuration")
			}
		})
	}
}

func TestSetupLoggingUsesConfigDirectory(t *testing.T) {
	// Given
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "custom.toml")
	// When
	setupLogging(appConfig{Log: logConfig{Size: 1}}, configPath)
	defer log.SetOutput(io.Discard)
	log.Print("custom config logging test")
	// Then
	if _, err := os.Stat(filepath.Join(configDir, "kobodeck.log")); err != nil {
		t.Fatalf("custom config log was not created: %v", err)
	}
}

func TestRunCheckMode(t *testing.T) {
	// Given
	outputDir := t.TempDir()
	cfg := appConfig{
		Server: serverConfig{URL: "https://readeck.example", Timeout: 5},
		Fetch:  fetchConfig{Workers: 2, Limit: 10, Labels: "tech"},
		Output: outputConfig{Path: outputDir, Delete: true},
	}
	bookmarks := []readeckBookmark{
		{ID: "included", Title: "Included article", Labels: []string{"TECH"}},
		{ID: "excluded", Title: "Excluded article", Labels: []string{"news"}},
	}
	// When
	var output bytes.Buffer
	if err := writeCheckOutput(&output, cfg, bookmarks); err != nil {
		t.Fatal(err)
	}
	// Then
	text := output.String()
	for _, fragment := range []string{
		"Configuration:",
		"URL:     https://readeck.example",
		"Output:  " + outputDir,
		"Connecting to Readeck... OK",
		"included — Included article",
		"1 bookmarks to sync, 1 skipped (label filter)",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("check output does not contain %q:\n%s", fragment, text)
		}
	}
	if strings.Contains(text, "excluded — Excluded article") {
		t.Fatalf("check output contains label-filtered bookmark:\n%s", text)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("--check created output files: %s", strings.Join(names, ", "))
	}
}

func TestAcquireLockRejectsSecondProcess(t *testing.T) {
	// Given
	lockFilePath := filepath.Join(t.TempDir(), "kobodeck.lock")
	first, err := acquireLock(lockFilePath)
	if err != nil {
		t.Fatalf("first acquireLock: %v", err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Errorf("close first lock: %v", err)
		}
	})
	// When
	second, err := acquireLock(lockFilePath)
	// Then
	if second != nil {
		if closeErr := second.Close(); closeErr != nil {
			t.Errorf("close second lock: %v", closeErr)
		}
		t.Fatal("second acquireLock unexpectedly succeeded")
	}
	if err == nil || err.Error() != "already running" {
		t.Fatalf("second acquireLock error = %v, want already running", err)
	}
}

func TestNickelRescanReportsEventFailure(t *testing.T) {
	// Given
	nickelStatusPath := filepath.Join(t.TempDir(), "nickel-status")
	if err := os.Mkdir(nickelStatusPath, 0o700); err != nil {
		t.Fatal(err)
	}
	// When
	err := nickelRescan(nickelStatusPath)
	// Then
	if err == nil || !strings.Contains(err.Error(), "add event: open "+nickelStatusPath) {
		t.Fatalf("nickelRescan() error = %v", err)
	}
}
