package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newValidAppConfig(outputPath string) appConfig {
	return appConfig{
		Server: serverConfig{URL: "https://readeck.example/api", Token: "token", Timeout: 5},
		Fetch:  fetchConfig{Workers: 2, Limit: 10, Status: "unread,reading"},
		Log:    logConfig{RetainedFiles: 10},
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
		{name: "zero retained logs", mutate: func(c *appConfig) { c.Log.RetainedFiles = 0 }},
		{name: "too many retained logs", mutate: func(c *appConfig) { c.Log.RetainedFiles = 101 }},
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

func TestRunCheckMode(t *testing.T) {
	// Given
	outputDir := t.TempDir()
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, `[{"id":"included","title":"Included article","labels":["TECH"]},{"id":"excluded","title":"Excluded article","labels":["news"]}]`); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(simulatedReadeckServer.Close)
	cfg := appConfig{
		Server: serverConfig{URL: simulatedReadeckServer.URL, Timeout: 5},
		Fetch:  fetchConfig{Workers: 2, Limit: 10, Labels: "tech"},
		Output: outputConfig{Path: outputDir, Delete: true},
	}
	// When
	var output bytes.Buffer
	application := app{cfg: cfg, readeck: newReadeckClient(simulatedReadeckServer.Client(), cfg.Server, false)}
	if err := application.runCheck(&output); err != nil {
		t.Fatal(err)
	}
	// Then
	text := output.String()
	for _, fragment := range []string{
		"Configuration:",
		"URL:     " + simulatedReadeckServer.URL,
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
