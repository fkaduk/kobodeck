package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	nativeTestBookmarkID     = "bookmark-1"
	nativeTestFavouriteShelf = "Native Favourites"
)

// TODO: this test does not only check for obvious behavior (redownloading the file), it also checks for http connections. brittle?
func TestDownloadSkipsExistingBookmark(t *testing.T) {
	var requests atomic.Int32
	// if the skip path works, this handler is never called.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "download should have been skipped", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	outputDir := t.TempDir()
	path := filepath.Join(outputDir, nativeTestBookmarkID+".kepub.epub")
	const original = "already converted"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := appConfig{
		Server: serverConfig{URL: server.URL, Token: "test-token"},
		Output: outputConfig{Path: outputDir},
	}
	readeck := newReadeckClient(server.Client(), cfg.Server, cfg.Log.Verbose)

	changed, err := readeck.downloadBookmarkFile(cfg.Output, readeckBookmark{ID: nativeTestBookmarkID, Updated: time.Now()})
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

// TODO: the above test is very complex. maybe accept some duplication instead of these abstraction layers.
// bookmarkAPIFixture is a tiny in-memory Readeck replacement. It records the
// number of GET and PATCH calls while mutating its bookmark state the same way
// Readeck would after accepting a PATCH.
type bookmarkAPIFixture struct {
	mu       sync.Mutex
	bookmark readeckBookmark
	patches  []map[string]any
	gets     int
}

func (fixture *bookmarkAPIFixture) handler(w http.ResponseWriter, r *http.Request) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()

	if r.URL.Path != "/api/bookmarks/"+nativeTestBookmarkID {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		fixture.gets++
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fixture.bookmark); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	case http.MethodPatch:
		var fields map[string]any
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fixture.patches = append(fixture.patches, fields)
		if progress, ok := fields["read_progress"].(float64); ok {
			fixture.bookmark.ReadProgress = int(progress)
		}
		if archived, ok := fields["is_archived"].(bool); ok {
			fixture.bookmark.IsArchived = archived
		}
		if marked, ok := fields["is_marked"].(bool); ok {
			fixture.bookmark.IsMarked = marked
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (fixture *bookmarkAPIFixture) snapshot() (readeckBookmark, int, int) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return fixture.bookmark, len(fixture.patches), fixture.gets
}

func createNickelFixture(t *testing.T, outputDir string, status bookStatus, inCollection bool) string {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join("testdata", "nickel-schema-176.sql"))
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "KoboReader.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		db.Close()
		t.Fatalf("create Nickel schema: %v", err)
	}
	contentID := nickelContentID(outputDir, nativeTestBookmarkID)
	if _, err := db.Exec(
		`INSERT INTO content (ContentID, ContentType, MimeType, ReadStatus, ___UserID)
		 VALUES (?, ?, ?, ?, ?)`,
		contentID, nickelContentTypeBook, "application/epub+zip", status, "native-test-user",
	); err != nil {
		db.Close()
		t.Fatalf("insert Nickel content: %v", err)
	}
	if inCollection {
		if _, err := db.Exec(
			`INSERT INTO Shelf (Id, InternalName, Name, _IsDeleted)
			 VALUES (?, ?, ?, 'false')`,
			"native-favourites", "native-favourites", nativeTestFavouriteShelf,
		); err != nil {
			db.Close()
			t.Fatalf("insert Nickel shelf: %v", err)
		}
		if _, err := db.Exec(
			`INSERT INTO ShelfContent (ShelfName, ContentId, _IsDeleted)
			 VALUES (?, ?, 'false')`,
			"native-favourites", contentID,
		); err != nil {
			db.Close()
			t.Fatalf("insert Nickel shelf content: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func TestReconcileLocalFiles(t *testing.T) {
	tests := []struct {
		name                string
		status              bookStatus
		inCollection        bool
		favouriteCollection string
		// valid means the bookmark appeared in the current Readeck fetch and is
		// therefore still eligible to remain on disk.
		valid bool
		// listedBookmark controls whether reconcileLocalFiles can use the
		// already-fetched bookmark data or must GET the bookmark from Readeck.
		listedBookmark bool
		// archive mirrors cfg.Sync.Archive: completed local reads should archive
		// the remote bookmark only when this option is enabled.
		archive bool
		// deleteLocal mirrors cfg.Output.Delete and allows stale unread files to
		// be removed from the device.
		deleteLocal bool
		// initial is the remote bookmark state before reconciliation.
		initial readeckBookmark
		// The remaining fields are the expected remote state, filesystem state,
		// changed flag, and HTTP call counts after reconciliation.
		wantReadProgress int
		wantArchived     bool
		wantMarked       bool
		wantFile         bool
		wantFilesChanged bool
		wantPatches      int
		wantGets         int
	}{
		{
			name:                "finished bookmark is read archived and favourited",
			status:              bookRead,
			inCollection:        true,
			favouriteCollection: nativeTestFavouriteShelf,
			valid:               true,
			listedBookmark:      true,
			archive:             true,
			initial:             readeckBookmark{ID: nativeTestBookmarkID},
			wantReadProgress:    100,
			wantArchived:        true,
			wantMarked:          true,
			wantFile:            true,
			wantPatches:         2,
		},
		{
			name:                "finished bookmark is read without archiving",
			status:              bookRead,
			favouriteCollection: nativeTestFavouriteShelf,
			valid:               true,
			listedBookmark:      true,
			initial:             readeckBookmark{ID: nativeTestBookmarkID},
			wantReadProgress:    100,
			wantFile:            true,
			wantPatches:         1,
		},
		{
			name:                "archived bookmark is unfavourited",
			status:              bookUnread,
			favouriteCollection: nativeTestFavouriteShelf,
			initial: readeckBookmark{
				ID: nativeTestBookmarkID, IsArchived: true, IsMarked: true,
			},
			wantArchived: true,
			wantFile:     true,
			wantPatches:  1,
			wantGets:     1,
		},
		{
			name:             "stale unread bookmark is deleted",
			status:           bookUnread,
			deleteLocal:      true,
			initial:          readeckBookmark{ID: nativeTestBookmarkID},
			wantFilesChanged: true,
		},
		{
			name:        "stale in-progress bookmark is retained",
			status:      bookReading,
			deleteLocal: true,
			initial:     readeckBookmark{ID: nativeTestBookmarkID},
			wantFile:    true,
		},
		{
			name:        "stale closed bookmark is retained",
			status:      bookClosed,
			deleteLocal: true,
			initial:     readeckBookmark{ID: nativeTestBookmarkID},
			wantFile:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Build an isolated local world for each case: one local kepub file,
			// one Nickel database, and one Readeck server.
			outputDir := t.TempDir()
			bookPath := filepath.Join(outputDir, nativeTestBookmarkID+".kepub.epub")
			if err := os.WriteFile(bookPath, []byte("native fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			nickelDBPath := createNickelFixture(t, outputDir, test.status, test.inCollection)

			// The fixture starts with the remote bookmark state described by the
			// table row and records every remote operation the reconciler makes.
			api := &bookmarkAPIFixture{bookmark: test.initial}
			server := httptest.NewServer(http.HandlerFunc(api.handler))
			t.Cleanup(server.Close)
			cfg := appConfig{
				Server: serverConfig{URL: server.URL, Token: "test-token"},
				Sync: syncConfig{
					Archive:             test.archive,
					FavouriteCollection: test.favouriteCollection,
				},
				Output: outputConfig{Path: outputDir, Delete: test.deleteLocal},
			}
			readeck := newReadeckClient(server.Client(), cfg.Server, cfg.Log.Verbose)
			// valid and bookmarks model the result of the earlier fetch phase:
			// valid tracks which ids still pass filters, while bookmarks carries
			// the bookmark records already available without another API GET.
			valid := make(map[string]bool)
			if test.valid {
				valid[nativeTestBookmarkID] = true
			}
			bookmarks := make(map[string]readeckBookmark)
			if test.listedBookmark {
				bookmarks[nativeTestBookmarkID] = test.initial
			}

			// This is the behavior under test: reconcile native device state with
			// Readeck state and report whether the local library changed.
			filesChanged := reconcileLocalFiles(readeck, cfg, valid, bookmarks, nickelDBPath)

			// Assert the final remote state first because these are the user-
			// visible sync effects: progress, archived status, and favourite mark.
			state, patches, gets := api.snapshot()
			if state.ReadProgress != test.wantReadProgress ||
				state.IsArchived != test.wantArchived || state.IsMarked != test.wantMarked {
				t.Fatalf("unexpected Readeck state: %+v", state)
			}
			// PATCH count protects against duplicate remote updates; GET count
			// protects the path that refetches stale bookmarks only when needed.
			if patches != test.wantPatches || gets != test.wantGets {
				t.Fatalf("API calls: got %d PATCH and %d GET, want %d PATCH and %d GET",
					patches, gets, test.wantPatches, test.wantGets)
			}
			// Finally verify local cleanup behavior and the boolean returned to
			// the caller, which controls whether Nickel is asked to rescan.
			_, statErr := os.Stat(bookPath)
			if test.wantFile && statErr != nil {
				t.Fatalf("local book was not retained: %v", statErr)
			}
			if !test.wantFile && !os.IsNotExist(statErr) {
				t.Fatalf("stale local book still exists: %v", statErr)
			}
			if filesChanged != test.wantFilesChanged {
				t.Fatalf("filesChanged = %t, want %t", filesChanged, test.wantFilesChanged)
			}
		})
	}
}

func TestNickelRescanReportsEventFailure(t *testing.T) {
	nickelStatusPath := filepath.Join(t.TempDir(), "nickel-status")
	if err := os.Mkdir(nickelStatusPath, 0o700); err != nil {
		t.Fatal(err)
	}

	err := nickelRescan(nickelStatusPath)
	if err == nil || !strings.Contains(err.Error(), "add event: open "+nickelStatusPath) {
		t.Fatalf("nickelRescan() error = %v", err)
	}
}

// TODO: reorder the tests by order of execution. I think this should be the very first test?
func TestAcquireLockRejectsSecondProcess(t *testing.T) {
	// Use a temp lock file so flock behavior is tested without touching the
	// device path used by production.
	lockFilePath := filepath.Join(t.TempDir(), "kobodeck.lock")

	// The first lock represents the running process and must remain open for the
	// second acquisition attempt to see the conflict.
	first, err := acquireLock(lockFilePath)
	if err != nil {
		t.Fatalf("first acquireLock: %v", err)
	}
	t.Cleanup(func() {
		first.Close()
	})
	// A second non-blocking flock on the same path should fail with the exact
	// sentinel error string main() logs for concurrent invocations.
	second, err := acquireLock(lockFilePath)
	if second != nil {
		second.Close()
		t.Fatal("second acquireLock unexpectedly succeeded")
	}
	if err == nil || err.Error() != "already running" {
		t.Fatalf("second acquireLock error = %v, want already running", err)
	}
}

func TestRunCheckOutput(t *testing.T) {
	// --check should validate config, contact Readeck once, apply label filters,
	// print a useful summary, and avoid creating output files.
	// This server exposes only the list endpoint used by runCheck. It also
	// verifies the bearer token so the test catches regressions in auth header
	// construction.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/bookmarks" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Labels deliberately differ by case to verify that the configured
		// lowercase filter still matches the included bookmark.
		_ = json.NewEncoder(w).Encode([]readeckBookmark{
			{ID: "included", Title: "Included article", Labels: []string{"TECH"}},
			{ID: "excluded", Title: "Excluded article", Labels: []string{"news"}},
		})
	}))
	t.Cleanup(server.Close)
	outputDir := t.TempDir()
	// Delete is enabled to prove --check remains read-only even when normal sync
	// would be allowed to remove stale local files.
	cfg := appConfig{
		Server: serverConfig{URL: server.URL, Token: "test-token", Timeout: 5},
		Fetch:  fetchConfig{Workers: 2, Limit: 10, Labels: "tech"},
		Output: outputConfig{Path: outputDir, Delete: true},
	}

	var output bytes.Buffer
	if err := runCheck(&output, cfg); err != nil {
		t.Fatalf("runCheck: %v", err)
	}
	text := output.String()
	// Assert stable fragments instead of the whole output so harmless formatting
	// around timestamps or ordering does not make this test brittle.
	for _, fragment := range []string{
		"Configuration:",
		"URL:     " + server.URL,
		"Output:  " + outputDir,
		"Connecting to Readeck... OK",
		"included — Included article",
		"1 bookmarks to sync, 1 skipped (label filter)",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("check output does not contain %q:\n%s", fragment, text)
		}
	}
	// The filtered bookmark should not appear in the human-readable plan at all.
	if strings.Contains(text, "excluded — Excluded article") {
		t.Fatalf("check output contains label-filtered bookmark:\n%s", text)
	}
	// --check must not download, convert, or delete anything. The temp output
	// directory starts empty, so any entry here is a side effect.
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
