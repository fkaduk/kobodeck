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

func resetConfig(t *testing.T) {
	// TODO: is this really necessary? i dont see how this does anything ?
	// cant i just reinstantiate a new instance every time?
	t.Helper()
	config = appConfig{}
}

// TODO: this test does not only check for obvious behavior (redownloading the file), it also checks for http connections. brittle?
func TestDownloadSkipsExistingBookmark(t *testing.T) {
	resetConfig(t)

	var requests atomic.Int32
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
	config = appConfig{
		Server: serverConfig{URL: server.URL, Token: "test-token"},
		Output: outputConfig{Path: outputDir},
	}

	changed, err := downloadBookmarkFile(server.Client(), readeckBookmark{ID: nativeTestBookmarkID, Updated: time.Now()})
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
		valid               bool
		listedBookmark      bool
		archive             bool
		deleteLocal         bool
		initial             readeckBookmark
		wantReadProgress    int
		wantArchived        bool
		wantMarked          bool
		wantFile            bool
		wantFilesChanged    bool
		wantPatches         int
		wantGets            int
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
			resetConfig(t)
			outputDir := t.TempDir()
			bookPath := filepath.Join(outputDir, nativeTestBookmarkID+".kepub.epub")
			if err := os.WriteFile(bookPath, []byte("native fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			nickelDBPath := createNickelFixture(t, outputDir, test.status, test.inCollection)

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
			config = cfg
			valid := make(map[string]bool)
			if test.valid {
				valid[nativeTestBookmarkID] = true
			}
			bookmarks := make(map[string]readeckBookmark)
			if test.listedBookmark {
				bookmarks[nativeTestBookmarkID] = test.initial
			}

			filesChanged := reconcileLocalFiles(server.Client(), cfg, valid, bookmarks, nickelDBPath)

			state, patches, gets := api.snapshot()
			if state.ReadProgress != test.wantReadProgress ||
				state.IsArchived != test.wantArchived || state.IsMarked != test.wantMarked {
				t.Fatalf("unexpected Readeck state: %+v", state)
			}
			if patches != test.wantPatches || gets != test.wantGets {
				t.Fatalf("API calls: got %d PATCH and %d GET, want %d PATCH and %d GET",
					patches, gets, test.wantPatches, test.wantGets)
			}
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
	resetConfig(t)
	nickelStatusPath := filepath.Join(t.TempDir(), "nickel-status")
	if err := os.Mkdir(nickelStatusPath, 0o700); err != nil {
		t.Fatal(err)
	}

	err := nickelRescan(nickelStatusPath)
	if err == nil || !strings.Contains(err.Error(), "add event: open "+nickelStatusPath) {
		t.Fatalf("nickelRescan() error = %v", err)
	}
}

func TestAcquireLockRejectsSecondProcess(t *testing.T) {
	resetConfig(t)
	lockFilePath := filepath.Join(t.TempDir(), "kobodeck.lock")

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
	if err == nil || err.Error() != "already running" {
		t.Fatalf("second acquireLock error = %v, want already running", err)
	}
}

func TestRunCheckOutput(t *testing.T) {
	resetConfig(t)

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
		_ = json.NewEncoder(w).Encode([]readeckBookmark{
			{ID: "included", Title: "Included article", Labels: []string{"TECH"}},
			{ID: "excluded", Title: "Excluded article", Labels: []string{"news"}},
		})
	}))
	t.Cleanup(server.Close)
	outputDir := t.TempDir()
	config = appConfig{
		Server: serverConfig{URL: server.URL, Token: "test-token", Timeout: 5},
		Fetch:  fetchConfig{Workers: 2, Limit: 10, Labels: "tech"},
		Output: outputConfig{Path: outputDir, Delete: true},
	}

	var output bytes.Buffer
	if err := runCheck(&output); err != nil {
		t.Fatalf("runCheck: %v", err)
	}
	text := output.String()
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
