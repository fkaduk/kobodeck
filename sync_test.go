package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestSyncRetriesTransientListBookmarksFailure verifies that a transient
// Readeck outage during the initial fetch is retried instead of failing the
// whole sync, which matters on the flaky device Wi-Fi.
func TestSyncRetriesTransientListBookmarksFailure(t *testing.T) {
	// Given
	var listCalls atomic.Int32
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/bookmarks" {
			http.NotFound(w, r)
			return
		}
		if listCalls.Add(1) == 1 {
			http.Error(w, "temporary outage", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode([]readeckBookmark{}); err != nil {
			t.Errorf("encode bookmarks: %v", err)
		}
	}))
	t.Cleanup(simulatedReadeckServer.Close)
	outputDir := t.TempDir()
	application := app{
		cfg: appConfig{
			Server: serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"},
			Fetch:  fetchConfig{Workers: 1},
			Log:    logConfig{RetainedFiles: 1},
			Output: outputConfig{Path: outputDir},
		},
		readeck:          newReadeckClient(simulatedReadeckServer.Client(), serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"}, false),
		nickel:           fakeNickelLibrary{},
		nickelStatusPath: filepath.Join(t.TempDir(), "nickel-status"),
	}
	// When
	err := application.sync(false)
	// Then
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := listCalls.Load(); got != 2 {
		t.Fatalf("listBookmarks called %d times, want 2 (initial failure + one retry)", got)
	}
}

// TestSyncTriggersRescanWhenFilesChange verifies that a changed local file
// triggers a Nickel rescan, which is what makes the newly-downloaded book
// appear in the device library.
func TestSyncTriggersRescanWhenFilesChange(t *testing.T) {
	// Given
	sourceEPUB := writeTestEPUB(t, filepath.Join(t.TempDir(), "source.epub"))
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/bookmarks":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode([]readeckBookmark{{ID: nativeTestBookmarkID, Updated: time.Now()}}); err != nil {
				t.Errorf("encode bookmarks: %v", err)
			}
		case "/api/bookmarks/" + nativeTestBookmarkID:
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(readeckBookmark{ID: nativeTestBookmarkID, Updated: time.Now()}); err != nil {
				t.Errorf("encode bookmark: %v", err)
			}
		case "/api/bookmarks/" + nativeTestBookmarkID + "/article.epub":
			w.Header().Set("Content-Type", "application/epub+zip")
			if _, err := w.Write(sourceEPUB); err != nil {
				t.Errorf("write epub: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(simulatedReadeckServer.Close)
	outputDir := t.TempDir()
	statusPath := filepath.Join(t.TempDir(), "nickel-status")
	// The firmware creates this file at boot; writeUSBEvent only appends to it.
	if err := os.WriteFile(statusPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	application := app{
		cfg: appConfig{
			Server: serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"},
			Fetch:  fetchConfig{Workers: 1},
			Log:    logConfig{RetainedFiles: 1},
			Output: outputConfig{Path: outputDir},
		},
		readeck:          newReadeckClient(simulatedReadeckServer.Client(), serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"}, false),
		nickel:           fakeNickelLibrary{},
		nickelStatusPath: statusPath,
	}
	// When
	err := application.sync(false)
	// Then
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	kepubPath := filepath.Join(outputDir, nativeTestBookmarkID+".kepub.epub")
	if _, err := os.Stat(kepubPath); err != nil {
		t.Fatalf("downloaded KEPUB missing: %v", err)
	}
	data, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("read rescan status file: %v", err)
	}
	if !strings.Contains(string(data), "usb plug add") || !strings.Contains(string(data), "usb plug remove") {
		t.Fatalf("rescan status file = %q, want add and remove events", string(data))
	}
}

func TestDownloadRunPreservesAllBookmarkFailures(t *testing.T) {
	// Given
	firstErr := errors.New("first download failed")
	secondErr := errors.New("second download failed")
	var completed atomic.Int32
	run := newDownloadRun(2, 3, func(entry readeckBookmark) (bool, error) {
		completed.Add(1)
		switch entry.ID {
		case "first":
			return false, firstErr
		case "second":
			return false, secondErr
		default:
			return true, nil
		}
	})
	for _, id := range []string{"first", "successful", "second"} {
		run.schedule(readeckBookmark{ID: id})
	}

	// When
	filesChanged, err := run.finish()

	// Then
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("download error = %v, want both failures", err)
	}
	for _, bookmarkID := range []string{"first", "second"} {
		if !strings.Contains(err.Error(), "bookmark "+bookmarkID) {
			t.Errorf("download error does not identify bookmark %s: %v", bookmarkID, err)
		}
	}
	if got := completed.Load(); got != 3 {
		t.Fatalf("completed downloads = %d, want 3", got)
	}
	if !filesChanged {
		t.Fatal("successful download was not reported as a filesystem change")
	}
}

// waitForAtLeast polls until counter reaches at least want, failing after a
// short deadline so a stuck test surfaces as a failure instead of a hang.
func waitForAtLeast(t *testing.T, counter *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for counter.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("counter did not reach %d", want)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestDownloadRunBoundsConcurrency verifies the worker limit: with more queued
// downloads than workers, at most workerLimit run at once.
func TestDownloadRunBoundsConcurrency(t *testing.T) {
	const workerLimit = 2
	const numDownloads = 5
	// Given
	var active, maxActive, completed atomic.Int32
	release := make(chan struct{})
	run := newDownloadRun(workerLimit, numDownloads, func(readeckBookmark) (bool, error) {
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		<-release
		active.Add(-1)
		completed.Add(1)
		return false, nil
	})
	// group.Go blocks its caller once the worker limit is reached, so the
	// blocking schedule loop runs in its own goroutine (mirroring how sync
	// schedules on the main goroutine); the blocked schedules form the queue.
	go func() {
		for i := 0; i < numDownloads; i++ {
			run.schedule(readeckBookmark{ID: strconv.Itoa(i)})
		}
	}()
	// When
	waitForAtLeast(t, &maxActive, workerLimit)
	// Then: exactly workerLimit were in flight while the rest waited for a slot.
	if got := maxActive.Load(); got != workerLimit {
		t.Fatalf("max concurrent downloads = %d, want %d", got, workerLimit)
	}
	close(release)
	waitForAtLeast(t, &completed, numDownloads)
	if got := maxActive.Load(); got > workerLimit {
		t.Fatalf("max concurrent downloads = %d, want <= %d", got, workerLimit)
	}
}
