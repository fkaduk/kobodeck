package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	nativeTestBookmarkID     = "bookmark-1"
	nativeTestFavouriteShelf = "Native Favourites"
)

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
	writeCheckOutput(&output, cfg, bookmarks)
	text := output.String()

	// Then
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

type fakeBookmarkStore struct {
	bookmark readeckBookmark
	patches  []map[string]any
	gets     int
}

func (store *fakeBookmarkStore) getBookmark(id string) (readeckBookmark, error) {
	store.gets++
	return store.bookmark, nil
}

func (store *fakeBookmarkStore) patchBookmark(id string, fields map[string]any) error {
	store.patches = append(store.patches, fields)
	if progress, ok := fields["read_progress"].(int); ok {
		store.bookmark.ReadProgress = progress
	}
	if archived, ok := fields["is_archived"].(bool); ok {
		store.bookmark.IsArchived = archived
	}
	if marked, ok := fields["is_marked"].(bool); ok {
		store.bookmark.IsMarked = marked
	}
	return nil
}

func TestAcquireLockRejectsSecondProcess(t *testing.T) {
	// Given
	lockFilePath := filepath.Join(t.TempDir(), "kobodeck.lock")

	// When
	first, err := acquireLock(lockFilePath)
	if err != nil {
		t.Fatalf("first acquireLock: %v", err)
	}
	t.Cleanup(func() {
		first.Close()
	})
	second, err := acquireLock(lockFilePath)
	if second != nil {
		second.Close()
		t.Fatal("second acquireLock unexpectedly succeeded")
	}

	// Then
	if err == nil || err.Error() != "already running" {
		t.Fatalf("second acquireLock error = %v, want already running", err)
	}
}

func TestDownloadSkipsExistingKepub(t *testing.T) {
	// Given
	var requests atomic.Int32
	simulatedReadeckServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			http.Error(w, "this handler should not have been called, download should have been skipped", http.StatusInternalServerError)
		}))
	t.Cleanup(simulatedReadeckServer.Close)

	outputDir := t.TempDir()
	path := filepath.Join(outputDir, nativeTestBookmarkID+".kepub.epub")
	const original = "already converted"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := appConfig{
		Server: serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"},
		Output: outputConfig{Path: outputDir},
	}
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
	if string(data) != original {
		t.Fatalf("existing bookmark was changed: got %q", data)
	}
}

type fakeNickelLibrary struct {
	status       bookStatus
	inCollection bool
}

func (library fakeNickelLibrary) readStatus(id, outputDir string) (bookStatus, error) {
	return library.status, nil
}

func (library fakeNickelLibrary) isInCollection(id, outputDir, collection string) (bool, error) {
	return library.inCollection, nil
}

type reconcileHarness struct {
	t              *testing.T
	readeck        fakeBookmarkStore
	nickel         fakeNickelLibrary
	cfg            appConfig
	book           localBook
	valid          bool
	listedBookmark bool
}

func newReconcileHarness(t *testing.T) *reconcileHarness {
	t.Helper()
	outputDir := t.TempDir()
	bookPath := filepath.Join(outputDir, nativeTestBookmarkID+".kepub.epub")
	if err := os.WriteFile(bookPath, []byte("native fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &reconcileHarness{
		t:       t,
		readeck: fakeBookmarkStore{bookmark: readeckBookmark{ID: nativeTestBookmarkID}},
		nickel:  fakeNickelLibrary{status: bookUnread},
		cfg:     appConfig{Output: outputConfig{Path: outputDir}},
		book:    localBook{id: nativeTestBookmarkID, path: bookPath},
	}
}

func (h *reconcileHarness) bookWasFetched() {
	h.valid = true
	h.listedBookmark = true
}

func (h *reconcileHarness) archiveReadBooks() {
	h.cfg.Sync.Archive = true
}

func (h *reconcileHarness) syncFavourites() {
	h.cfg.Sync.FavouriteCollection = nativeTestFavouriteShelf
}

func (h *reconcileHarness) bookIsInKoboCollection() {
	h.nickel.inCollection = true
}

func (h *reconcileHarness) deleteStaleFiles() {
	h.cfg.Output.Delete = true
}

func (h *reconcileHarness) remoteBookmark(bookmark readeckBookmark) {
	if bookmark.ID == "" {
		bookmark.ID = nativeTestBookmarkID
	}
	h.readeck.bookmark = bookmark
}

type reconcileResult struct {
	state        readeckBookmark
	filesChanged bool
	patches      int
	gets         int
	removed      bool
}

func (h *reconcileHarness) run(status bookStatus) reconcileResult {
	h.t.Helper()
	h.nickel.status = status

	valid := make(map[string]bool)
	if h.valid {
		valid[nativeTestBookmarkID] = true
	}
	bookmarks := make(map[string]readeckBookmark)
	if h.listedBookmark {
		bookmarks[nativeTestBookmarkID] = h.readeck.bookmark
	}

	filesChanged, err := reconcileLocalBook(
		&h.readeck,
		h.nickel,
		h.cfg,
		h.cfg.Output.Path,
		h.book,
		valid,
		bookmarks,
	)
	if err != nil {
		h.t.Fatal(err)
	}
	_, statErr := os.Stat(h.book.path)
	return reconcileResult{
		state:        h.readeck.bookmark,
		filesChanged: filesChanged,
		patches:      len(h.readeck.patches),
		gets:         h.readeck.gets,
		removed:      os.IsNotExist(statErr),
	}
}

func requireReadeckState(t *testing.T, got readeckBookmark, readProgress int, archived, marked bool) {
	t.Helper()
	if got.ReadProgress != readProgress || got.IsArchived != archived || got.IsMarked != marked {
		t.Fatalf("unexpected Readeck state: %+v", got)
	}
}

func requireAPICalls(t *testing.T, got reconcileResult, patches, gets int) {
	t.Helper()
	if got.patches != patches || got.gets != gets {
		t.Fatalf("API calls: got %d PATCH and %d GET, want %d PATCH and %d GET",
			got.patches, got.gets, patches, gets)
	}
}

func requireLocalFile(t *testing.T, got reconcileResult, wantFile bool) {
	t.Helper()
	if wantFile && got.removed {
		t.Fatal("local book was removed")
	}
	if !wantFile && !got.removed {
		t.Fatal("local book was not removed")
	}
}

func TestReconcileMarksFinishedBookmarkReadArchivedAndFavourited(t *testing.T) {
	// Given
	reconcile := newReconcileHarness(t)
	reconcile.bookWasFetched()
	reconcile.archiveReadBooks()
	reconcile.syncFavourites()
	reconcile.bookIsInKoboCollection()

	// When
	got := reconcile.run(bookRead)

	// Then
	requireReadeckState(t, got.state, 100, true, true)
	requireAPICalls(t, got, 2, 0)
	requireLocalFile(t, got, true)
}

func TestReconcileMarksFinishedBookmarkReadWithoutArchiving(t *testing.T) {
	// Given
	reconcile := newReconcileHarness(t)
	reconcile.bookWasFetched()
	reconcile.syncFavourites()

	// When
	got := reconcile.run(bookRead)

	// Then
	requireReadeckState(t, got.state, 100, false, false)
	requireAPICalls(t, got, 1, 0)
	requireLocalFile(t, got, true)
}

func TestReconcileUnfavoursArchivedBookmark(t *testing.T) {
	// Given
	reconcile := newReconcileHarness(t)
	reconcile.syncFavourites()
	reconcile.remoteBookmark(readeckBookmark{
		IsArchived: true,
		IsMarked:   true,
	})

	// When
	got := reconcile.run(bookUnread)

	// Then
	requireReadeckState(t, got.state, 0, true, false)
	requireAPICalls(t, got, 1, 1)
	requireLocalFile(t, got, true)
}

func TestReconcileDeletesStaleUnreadBookmark(t *testing.T) {
	// Given
	reconcile := newReconcileHarness(t)
	reconcile.deleteStaleFiles()

	// When
	got := reconcile.run(bookUnread)

	// Then
	requireLocalFile(t, got, false)
	if !got.filesChanged {
		t.Fatal("deleted stale bookmark was not reported as a filesystem change")
	}
}

func TestReconcileKeepsStaleInProgressBookmark(t *testing.T) {
	// Given
	reconcile := newReconcileHarness(t)
	reconcile.deleteStaleFiles()

	// When
	got := reconcile.run(bookReading)

	// Then
	requireLocalFile(t, got, true)
	if got.filesChanged {
		t.Fatal("retained in-progress bookmark was reported as a filesystem change")
	}
}

func TestReconcileKeepsStaleClosedBookmark(t *testing.T) {
	// Given
	reconcile := newReconcileHarness(t)
	reconcile.deleteStaleFiles()

	// When
	got := reconcile.run(bookClosed)

	// Then
	requireLocalFile(t, got, true)
	if got.filesChanged {
		t.Fatal("retained closed bookmark was reported as a filesystem change")
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
