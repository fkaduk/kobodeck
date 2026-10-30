package main

import (
	"bytes"
	"encoding/json"
	"io"
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

func TestListBookmarksPaginatesFiltersAndAppliesLimit(t *testing.T) {
	// Given
	var offsets []string
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offsets = append(offsets, r.URL.Query().Get("offset"))
		if got := r.URL.Query().Get("read_status"); got != "unread" {
			t.Errorf("read_status = %q, want unread", got)
		}
		pageSize := 100
		if r.URL.Query().Get("offset") == "100" {
			pageSize = 10
		}
		entries := make([]readeckBookmark, pageSize)
		for i := range entries {
			entries[i].ID = strconv.Itoa(len(offsets)*100 + i)
		}
		if err := json.NewEncoder(w).Encode(entries); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(simulatedReadeckServer.Close)
	client := newReadeckClient(simulatedReadeckServer.Client(), serverConfig{URL: simulatedReadeckServer.URL}, false)
	// When
	entries, err := client.listBookmarks(fetchConfig{Limit: 101, Status: "unread"})
	// Then
	if err != nil {
		t.Fatalf("listBookmarks: %v", err)
	}
	if len(entries) != 101 {
		t.Fatalf("listBookmarks returned %d entries, want 101", len(entries))
	}
	if strings.Join(offsets, ",") != "0,100" {
		t.Fatalf("requested offsets %v, want [0 100]", offsets)
	}
}

// TestGetBookmark verifies the single-bookmark fetch: request method, path,
// auth, and the decoded fields.
func TestGetBookmark(t *testing.T) {
	// Given
	var gotPath, gotMethod, gotAuth string
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(readeckBookmark{ID: nativeTestBookmarkID, Title: "Some article", ReadProgress: 42, IsMarked: true}); err != nil {
			t.Errorf("encode bookmark: %v", err)
		}
	}))
	t.Cleanup(simulatedReadeckServer.Close)
	client := newReadeckClient(simulatedReadeckServer.Client(), serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"}, false)
	// When
	bookmark, err := client.getBookmark(nativeTestBookmarkID)
	// Then
	if err != nil {
		t.Fatalf("getBookmark: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/bookmarks/"+nativeTestBookmarkID {
		t.Fatalf("request %s %s, want GET /api/bookmarks/%s", gotMethod, gotPath, nativeTestBookmarkID)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	if bookmark.ID != nativeTestBookmarkID || bookmark.Title != "Some article" || bookmark.ReadProgress != 42 || !bookmark.IsMarked {
		t.Fatalf("getBookmark = %+v", bookmark)
	}
}

// TestGetBookmarkError verifies a non-2xx response surfaces as an error.
func TestGetBookmarkError(t *testing.T) {
	// Given
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(simulatedReadeckServer.Close)
	client := newReadeckClient(simulatedReadeckServer.Client(), serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"}, false)
	// When
	_, err := client.getBookmark(nativeTestBookmarkID)
	// Then
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("getBookmark error = %v, want a 404 error", err)
	}
}

// TestPatchBookmark verifies only the provided fields are sent to Readeck via
// PATCH, with the correct method, path, and auth.
func TestPatchBookmark(t *testing.T) {
	// Given
	var gotPath, gotMethod, gotAuth, gotBody string
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(simulatedReadeckServer.Close)
	client := newReadeckClient(simulatedReadeckServer.Client(), serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"}, false)
	// When
	err := client.patchBookmark(nativeTestBookmarkID, map[string]any{"read_progress": 100, "is_archived": true})
	// Then
	if err != nil {
		t.Fatalf("patchBookmark: %v", err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/api/bookmarks/"+nativeTestBookmarkID {
		t.Fatalf("request %s %s, want PATCH /api/bookmarks/%s", gotMethod, gotPath, nativeTestBookmarkID)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(gotBody), &decoded); err != nil {
		t.Fatalf("decode patch body %q: %v", gotBody, err)
	}
	if got, want := decoded["read_progress"], float64(100); got != want {
		t.Fatalf("patch body read_progress = %v, want %v", got, want)
	}
	if got, want := decoded["is_archived"], true; got != want {
		t.Fatalf("patch body is_archived = %v, want %v", got, want)
	}
	if len(decoded) != 2 {
		t.Fatalf("patch body has %d fields, want exactly read_progress and is_archived", len(decoded))
	}
}

// TestPatchBookmarkError verifies a non-2xx response surfaces as an error.
func TestPatchBookmarkError(t *testing.T) {
	// Given
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	t.Cleanup(simulatedReadeckServer.Close)
	client := newReadeckClient(simulatedReadeckServer.Client(), serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"}, false)
	// When
	err := client.patchBookmark(nativeTestBookmarkID, map[string]any{"read_progress": 100})
	// Then
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("patchBookmark error = %v, want a 500 error", err)
	}
}

func TestDownloadSkipsExistingKepub(t *testing.T) {
	// Given
	var requests atomic.Int32
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "this handler should not have been called, download should have been skipped", http.StatusInternalServerError)
	}))
	t.Cleanup(simulatedReadeckServer.Close)

	outputDir := t.TempDir()
	path := filepath.Join(outputDir, nativeTestBookmarkID+".kepub.epub")
	original := writeTestEPUB(t, path)
	cfg := appConfig{Server: serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"}, Output: outputConfig{Path: outputDir}}
	readeck := newReadeckClient(simulatedReadeckServer.Client(), cfg.Server, cfg.Log.Verbose)
	// When
	changed, err := readeck.downloadBookmarkFile(cfg.Output, readeckBookmark{ID: nativeTestBookmarkID, Updated: time.Now()})
	// Then
	if err != nil {
		t.Fatalf("download existing bookmark: %v", err)
	}
	if changed {
		t.Fatal("existing bookmark was reported as a filesystem change")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("existing bookmark made %d HTTP requests", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, original) {
		t.Fatalf("existing bookmark was changed: got %q", data)
	}
}

func TestDownloadDoesNotSkipInvalidExistingKepub(t *testing.T) {
	// Given
	var requests atomic.Int32
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "download attempted", http.StatusInternalServerError)
	}))
	t.Cleanup(simulatedReadeckServer.Close)

	outputDir := t.TempDir()
	path := filepath.Join(outputDir, nativeTestBookmarkID+".kepub.epub")
	if err := os.WriteFile(path, []byte("non-empty but invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := appConfig{Server: serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"}, Output: outputConfig{Path: outputDir}}
	readeck := newReadeckClient(simulatedReadeckServer.Client(), cfg.Server, cfg.Log.Verbose)
	// When
	_, err := readeck.downloadBookmarkFile(cfg.Output, readeckBookmark{ID: nativeTestBookmarkID})
	// Then
	if err == nil {
		t.Fatal("download with failed HTTP response unexpectedly succeeded")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("invalid existing KEPUB made %d HTTP requests, want 1", got)
	}
}

func TestDownloadInstallsKepubUsingBookmarkID(t *testing.T) {
	// Given
	sourcePath := filepath.Join(t.TempDir(), "source.epub")
	epub := writeTestEPUB(t, sourcePath)
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/bookmarks/"+nativeTestBookmarkID+"/article.epub" {
			t.Errorf("download path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/epub+zip")
		if _, err := w.Write(epub); err != nil {
			t.Errorf("write EPUB response: %v", err)
		}
	}))
	t.Cleanup(simulatedReadeckServer.Close)

	outputDir := t.TempDir()
	cfg := appConfig{Server: serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"}, Output: outputConfig{Path: outputDir}}
	readeck := newReadeckClient(simulatedReadeckServer.Client(), cfg.Server, cfg.Log.Verbose)
	wantPath := filepath.Join(outputDir, nativeTestBookmarkID+".kepub.epub")
	// When
	changed, err := readeck.downloadBookmarkFile(cfg.Output, readeckBookmark{ID: nativeTestBookmarkID, Updated: time.Now()})
	// Then
	if err != nil {
		t.Fatalf("download bookmark: %v", err)
	}
	if !changed {
		t.Fatal("downloaded bookmark was not reported as a filesystem change")
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("downloaded bookmark path: %v", err)
	}
	if err := validateEPUB(wantPath); err != nil {
		t.Fatalf("downloaded bookmark is invalid: %v", err)
	}
}

func TestDownloadRejectsUnsafeBookmarkID(t *testing.T) {
	// Given
	var requests atomic.Int32
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	t.Cleanup(simulatedReadeckServer.Close)
	outputDir := t.TempDir()
	cfg := appConfig{Server: serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"}, Output: outputConfig{Path: outputDir}}
	readeck := newReadeckClient(simulatedReadeckServer.Client(), cfg.Server, cfg.Log.Verbose)
	// When
	_, err := readeck.downloadBookmarkFile(cfg.Output, readeckBookmark{ID: "../escape"})
	// Then
	if err == nil {
		t.Fatal("unsafe bookmark ID unexpectedly accepted")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("unsafe bookmark ID made %d HTTP requests, want 0", got)
	}
}

func TestAPIResponseSizeLimit(t *testing.T) {
	// Given
	simulatedReadeckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(maxAPIResponseSize+1))
	}))
	t.Cleanup(simulatedReadeckServer.Close)
	readeck := newReadeckClient(simulatedReadeckServer.Client(), serverConfig{URL: simulatedReadeckServer.URL}, false)
	// When
	_, err := readeck.doAPIRequest(http.MethodGet, simulatedReadeckServer.URL, nil)
	// Then
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized API response error = %v, want size-limit error", err)
	}
}
