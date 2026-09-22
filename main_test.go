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
