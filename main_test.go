package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
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

func validAppConfig(outputPath string) appConfig {
	return appConfig{
		Server: serverConfig{URL: "https://readeck.example/api", Token: "token", Timeout: 5},
		Fetch:  fetchConfig{Workers: 2, Limit: 10, Status: "unread,reading"},
		Log:    logConfig{Size: 1},
		Output: outputConfig{Path: outputPath},
	}
}

func TestAppConfigValidation(t *testing.T) {
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
		{name: "empty status", mutate: func(c *appConfig) { c.Fetch.Status = "" }, valid: true},
		{name: "valid statuses", mutate: func(c *appConfig) { c.Fetch.Status = "unread, reading,read" }, valid: true},
		{name: "invalid status", mutate: func(c *appConfig) { c.Fetch.Status = "unread,finished" }},
		{name: "negative limit", mutate: func(c *appConfig) { c.Fetch.Limit = -1 }},
		{name: "too many workers", mutate: func(c *appConfig) { c.Fetch.Workers = 33 }},
		{name: "negative log size", mutate: func(c *appConfig) { c.Log.Size = -1 }},
		{name: "relative output", mutate: func(c *appConfig) { c.Output.Path = "books" }},
		{name: "root output", mutate: func(c *appConfig) { c.Output.Path = "/" }},
		{name: "absolute output", mutate: func(c *appConfig) { c.Output.Path = outputPath }, valid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validAppConfig(outputPath)
			tt.mutate(&cfg)
			err := cfg.validate()
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
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "custom.toml")
	setupLogging(appConfig{Log: logConfig{Size: 1}}, configPath)
	defer log.SetOutput(io.Discard)
	log.Print("custom config logging test")
	if _, err := os.Stat(filepath.Join(configDir, "kobodeck.log")); err != nil {
		t.Fatalf("custom config log was not created: %v", err)
	}
}

const (
	nativeTestBookmarkID     = "bookmark-1"
	nativeTestFavouriteShelf = "Native Favourites"
)

func writeTestEPUB(t *testing.T, path string) []byte {
	t.Helper()
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	mimetype, err := w.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mimetype.Write([]byte("application/epub+zip")); err != nil {
		t.Fatal(err)
	}
	container, err := w.Create("META-INF/container.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := container.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`)); err != nil {
		t.Fatal(err)
	}
	content, err := w.Create("OEBPS/content.opf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := content.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="bookid">test</dc:identifier><dc:title>Test</dc:title><dc:language>en</dc:language></metadata>
  <manifest><item id="chapter" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="chapter"/></spine>
</package>`)); err != nil {
		t.Fatal(err)
	}
	chapter, err := w.Create("OEBPS/chapter.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chapter.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml"><head><title>Test</title></head><body><p>Hello</p></body></html>`)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
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
	patchErr error
}

func (store *fakeBookmarkStore) getBookmark(id string) (readeckBookmark, error) {
	store.gets++
	return store.bookmark, nil
}

func (store *fakeBookmarkStore) patchBookmark(id string, fields map[string]any) error {
	store.patches = append(store.patches, fields)
	if store.patchErr != nil {
		return store.patchErr
	}
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
	original := writeTestEPUB(t, path)
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
	if !bytes.Equal(data, original) {
		t.Fatalf("existing bookmark was changed: got %q", data)
	}
}

func TestDownloadDoesNotSkipInvalidExistingKepub(t *testing.T) {
	// Given
	var requests atomic.Int32
	simulatedReadeckServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			http.Error(w, "download attempted", http.StatusInternalServerError)
		}))
	t.Cleanup(simulatedReadeckServer.Close)

	outputDir := t.TempDir()
	path := filepath.Join(outputDir, nativeTestBookmarkID+".kepub.epub")
	if err := os.WriteFile(path, []byte("non-empty but invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := appConfig{
		Server: serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"},
		Output: outputConfig{Path: outputDir},
	}
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
		_, _ = w.Write(epub)
	}))
	t.Cleanup(simulatedReadeckServer.Close)

	outputDir := t.TempDir()
	cfg := appConfig{
		Server: serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"},
		Output: outputConfig{Path: outputDir},
	}
	readeck := newReadeckClient(simulatedReadeckServer.Client(), cfg.Server, cfg.Log.Verbose)
	wantPath := filepath.Join(outputDir, nativeTestBookmarkID+".kepub.epub")

	// When
	changed, err := readeck.downloadBookmarkFile(cfg.Output, readeckBookmark{
		ID:      nativeTestBookmarkID,
		Updated: time.Now(),
	})

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
	simulatedReadeckServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
		}))
	t.Cleanup(simulatedReadeckServer.Close)
	outputDir := t.TempDir()
	cfg := appConfig{
		Server: serverConfig{URL: simulatedReadeckServer.URL, Token: "test-token"},
		Output: outputConfig{Path: outputDir},
	}
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
	simulatedReadeckServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestToKepubAtomicallyInstallsConvertedBook(t *testing.T) {
	// Given
	outputDir := t.TempDir()
	source := filepath.Join(outputDir, "article.epub")
	destination := filepath.Join(outputDir, "article.kepub.epub")
	writeTestEPUB(t, source)

	// When
	kepubPath, err := toKepub(source, destination)

	// Then
	if err != nil {
		t.Fatalf("toKepub: %v", err)
	}
	if err := validateEPUB(kepubPath); err != nil {
		t.Fatalf("converted KEPUB is invalid: %v", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source EPUB still exists: %v", err)
	}
	tmpFiles, err := filepath.Glob(filepath.Join(outputDir, ".article.kepub.epub.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpFiles) != 0 {
		t.Fatalf("conversion temporary files remain: %v", tmpFiles)
	}
}

func TestToKepubFailurePreservesExistingBook(t *testing.T) {
	// Given
	outputDir := t.TempDir()
	source := filepath.Join(outputDir, "article.epub")
	final := filepath.Join(outputDir, "article.kepub.epub")
	if err := os.WriteFile(source, []byte("invalid source"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := writeTestEPUB(t, final)

	// When
	if _, err := toKepub(source, final); err == nil {
		t.Fatal("invalid source unexpectedly converted")
	}

	// Then
	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatal("existing KEPUB was changed after failed conversion")
	}
}

func TestListLocalBooksOnlyListsKepubsInOutputDirectory(t *testing.T) {
	// Given
	outputDir := t.TempDir()
	kepubPath := filepath.Join(outputDir, "bookmark-1.kepub.epub")
	if err := os.WriteFile(kepubPath, []byte("kepub"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "ordinary.epub"), []byte("epub"), 0o600); err != nil {
		t.Fatal(err)
	}
	nestedDir := filepath.Join(outputDir, "nested")
	if err := os.Mkdir(nestedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "nested.kepub.epub"), []byte("kepub"), 0o600); err != nil {
		t.Fatal(err)
	}

	// When
	books, err := listLocalBooks(outputDir)
	if err != nil {
		t.Fatal(err)
	}

	// Then
	if len(books) != 1 {
		t.Fatalf("listLocalBooks returned %d books, want 1: %+v", len(books), books)
	}
	if books[0].id != "bookmark-1" || books[0].path != kepubPath {
		t.Fatalf("listLocalBooks returned %+v, want bookmark-1 at %s", books[0], kepubPath)
	}
}

type fakeNickelLibrary struct {
	status        bookStatus
	inCollection  bool
	statusErr     error
	collectionErr error
}

func (library fakeNickelLibrary) readStatus(id, outputDir string) (bookStatus, error) {
	return library.status, library.statusErr
}

func (library fakeNickelLibrary) isInCollection(id, outputDir, collection string) (bool, error) {
	return library.inCollection, library.collectionErr
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
		h.cfg.Output.Delete,
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

func TestReconcileMarksFinishedBookOutsideFetchedFeedRead(t *testing.T) {
	// Given
	reconcile := newReconcileHarness(t)
	reconcile.archiveReadBooks()

	// When
	got := reconcile.run(bookRead)

	// Then
	requireReadeckState(t, got.state, 100, true, false)
	requireAPICalls(t, got, 1, 1)
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

func TestReconcileDoesNotDeleteWhenDeletionIsDisallowed(t *testing.T) {
	// Given
	reconcile := newReconcileHarness(t)
	reconcile.deleteStaleFiles()
	valid := make(map[string]bool)
	bookmarks := make(map[string]readeckBookmark)

	// When
	filesChanged, err := reconcileLocalFiles(
		&reconcile.readeck,
		reconcile.nickel,
		reconcile.cfg,
		valid,
		bookmarks,
		false,
	)
	if err != nil {
		t.Fatalf("reconcileLocalFiles: %v", err)
	}

	// Then
	if _, err := os.Stat(reconcile.book.path); err != nil {
		t.Fatalf("stale local book was deleted: %v", err)
	}
	if filesChanged {
		t.Fatal("retained stale bookmark was reported as a filesystem change")
	}
}

func TestReconcilePropagatesStatusError(t *testing.T) {
	// Given
	reconcile := newReconcileHarness(t)
	reconcile.nickel.statusErr = errors.New("status unavailable")

	// When
	_, err := reconcileLocalBook(
		&reconcile.readeck,
		reconcile.nickel,
		reconcile.cfg,
		reconcile.cfg.Output.Path,
		reconcile.book,
		make(map[string]bool),
		make(map[string]readeckBookmark),
		false,
	)

	// Then
	if err == nil || !strings.Contains(err.Error(), "status unavailable") {
		t.Fatalf("status error = %v, want propagated status error", err)
	}
}

func TestReconcilePropagatesRemotePatchError(t *testing.T) {
	// Given
	reconcile := newReconcileHarness(t)
	reconcile.bookWasFetched()
	reconcile.nickel.status = bookRead
	reconcile.readeck.patchErr = errors.New("remote update failed")

	// When
	_, err := reconcileLocalBook(
		&reconcile.readeck,
		reconcile.nickel,
		reconcile.cfg,
		reconcile.cfg.Output.Path,
		reconcile.book,
		map[string]bool{nativeTestBookmarkID: true},
		map[string]readeckBookmark{nativeTestBookmarkID: reconcile.readeck.bookmark},
		false,
	)

	// Then
	if err == nil || !strings.Contains(err.Error(), "remote update failed") {
		t.Fatalf("patch error = %v, want propagated remote update error", err)
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
