// Integration tests run against a real Readeck instance
// spun up in Docker via testcontainers-go.
// They cover the full sync flow: listing bookmarks, downloading EPUBs,
// reading Nickel DB status, archiving back to Readeck, and local file cleanup.

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	readeckImage   = "codeberg.org/readeck/readeck:latest"
	testAdminUser  = "testadmin"
	testAdminPass  = "testpass123"
	testAdminEmail = "testadmin@test.invalid"
	// A stable URL with real extractable content for EPUB download tests.
	testBookmarkURL = "https://example.com"
)

var bearerRegexp = regexp.MustCompile(`value="Authorization: Bearer ([^"]+)"`)

// TestMain starts a Readeck container, bootstraps it with an admin user and
// API token, configures the global config, then runs the integration tests.
func TestMain(m *testing.M) {
	ctx := context.Background()

	ctr, err := testcontainers.Run(ctx, readeckImage,
		testcontainers.WithWaitStrategy(
			wait.ForExec([]string{"readeck", "healthcheck"}).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start readeck container: %v\n", err)
		os.Exit(1)
	}
	defer ctr.Terminate(ctx)

	// Create the admin user via the readeck CLI inside the container.
	if _, _, err = ctr.Exec(ctx, []string{
		"readeck", "user",
		"-user", testAdminUser,
		"-password", testAdminPass,
		"-email", testAdminEmail,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create admin user: %v\n", err)
		os.Exit(1)
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get container host: %v\n", err)
		os.Exit(1)
	}
	port, err := ctr.MappedPort(ctx, "8000/tcp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get mapped port: %v\n", err)
		os.Exit(1)
	}
	baseURL := fmt.Sprintf("http://%s:%s", host, port.Port())

	// Bootstrap an API token via the web UI (there is no token creation CLI).
	token, err := bootstrapToken(baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to bootstrap API token: %v\n", err)
		os.Exit(1)
	}

	if err := loadConfig("kobodeck.toml"); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load kobodeck.toml: %v\n", err)
		os.Exit(1)
	}
	config.Server.URL = baseURL
	config.Server.Token = token

	os.Exit(m.Run())
}

// --- Helpers ---

// bootstrapToken logs in via the Readeck web UI and creates an API token,
// returning the raw token string. Token creation has no CLI equivalent in
// Readeck v0.22+, so we use a web session.
func bootstrapToken(baseURL string) (string, error) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// POST /login — sets the session cookie on success (303 → homepage).
	resp, err := client.PostForm(baseURL+"/login", url.Values{
		"username": {testAdminUser},
		"password": {testAdminPass},
		"redirect": {""},
	})
	if err != nil {
		return "", fmt.Errorf("login request: %w", err)
	}
	resp.Body.Close()

	// POST /profile/tokens — Readeck creates the token and 303-redirects to
	// its detail page. The http.Client follows the redirect automatically,
	// converting it to a GET, so the response body is the detail page HTML.
	resp, err = client.Post(baseURL+"/profile/tokens", "application/x-www-form-urlencoded", nil)
	if err != nil {
		return "", fmt.Errorf("token creation request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading token page: %w", err)
	}

	// The detail page renders the token as:
	//   value="Authorization: Bearer <token>"
	matches := bearerRegexp.FindSubmatch(body)
	if len(matches) < 2 {
		return "", fmt.Errorf("token not found in profile page (check login)")
	}
	return string(matches[1]), nil
}

// apiRequest is a small helper that sends an authenticated request to the
// Readeck instance under test and returns the response.
func apiRequest(t *testing.T, method, path string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, config.Server.URL+path, body)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+config.Server.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	return resp
}

// createLoadedBookmark posts a bookmark and blocks until Readeck has fetched and
// parsed it (loaded: true). It registers a cleanup that deletes the bookmark.
func createLoadedBookmark(t *testing.T, bookmarkURL string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"url": bookmarkURL})
	resp := apiRequest(t, http.MethodPost, "/api/bookmarks", bytes.NewBuffer(body))
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("create bookmark: expected 202, got %d", resp.StatusCode)
	}
	id := resp.Header.Get("Bookmark-Id")
	if id == "" {
		t.Fatal("create bookmark: missing Bookmark-Id header")
	}

	// Poll the specific bookmark until Readeck has fetched and parsed it.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp = apiRequest(t, http.MethodGet, "/api/bookmarks/"+id, nil)
		var bm struct {
			Loaded bool `json:"loaded"`
		}
		json.NewDecoder(resp.Body).Decode(&bm)
		resp.Body.Close()
		if bm.Loaded {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Cleanup(func() {
		resp := apiRequest(t, http.MethodDelete, "/api/bookmarks/"+id, nil)
		resp.Body.Close()
	})
	return id
}

const nickelSchema = "testdata/nickel-schema-176.sql"

// createDB creates a SQLite database at dbPath from schema.
func createDB(t *testing.T, dbPath string, schema string) {
	t.Helper()
	data, err := os.ReadFile(schema)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	defer db.Close()
	if _, err = db.Exec(string(data)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
}

// captureLog redirects the global logger into a buffer for the duration of the
// test and returns a function that reads what was captured.
func captureLog(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return func() string { return buf.String() }
}

// --- Tests ---

// TestSmoke verifies that the container is up, authentication works, and the
// bookmark round-trip (create → list → delete) succeeds.
func TestSmoke(t *testing.T) {
	id := createLoadedBookmark(t, testBookmarkURL)
	t.Logf("bookmark loaded: %s", id)

	resp := apiRequest(t, http.MethodGet, "/api/bookmarks/"+id, nil)
	defer resp.Body.Close()
	var bm struct {
		URL    string `json:"url"`
		Loaded bool   `json:"loaded"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bm); err != nil {
		t.Fatalf("decode bookmark: %v", err)
	}
	if bm.URL != testBookmarkURL {
		t.Errorf("bookmark URL: got %q, want %q", bm.URL, testBookmarkURL)
	}
	if !bm.Loaded {
		t.Error("bookmark should be loaded")
	}
}

// TestCheckMode verifies that runCheck connects to Readeck and reports
// the expected bookmark in its output.
func TestCheckMode(t *testing.T) {
	id := createLoadedBookmark(t, testBookmarkURL)

	var buf bytes.Buffer
	if err := runCheck(&buf); err != nil {
		t.Fatalf("runCheck: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, id) {
		t.Errorf("runCheck output does not contain bookmark ID %s:\n%s", id, out)
	}
	if !strings.Contains(out, "OK") {
		t.Errorf("runCheck output missing connection OK:\n%s", out)
	}
}

// setupSyncEnv creates a temp output dir and Nickel DB, saves the full config
// and nickelDBPath, and restores them on cleanup.
func setupSyncEnv(t *testing.T) (outputDir, dbPath string) {
	t.Helper()
	outputDir = t.TempDir()
	dbPath = filepath.Join(t.TempDir(), "KoboReader.sqlite")
	createDB(t, dbPath, nickelSchema)
	savedConfig := config
	savedDB := nickelDBPath
	t.Cleanup(func() {
		config = savedConfig
		nickelDBPath = savedDB
	})
	config.Output.Path = outputDir
	nickelDBPath = dbPath
	return
}

// downloadEntry finds bookmark id in the unread feed, downloads it, and
// returns the local kepub path.
func downloadEntry(t *testing.T, id string) string {
	t.Helper()
	client := &http.Client{Timeout: 30 * time.Second}
	entries, err := listBookmarks(client)
	if err != nil {
		t.Fatalf("listBookmarks: %v", err)
	}
	for _, e := range entries {
		if e.ID == id {
			client := &http.Client{Timeout: 30 * time.Second}
			if err := download(client, e); err != nil {
				t.Fatalf("download %s: %v", id, err)
			}
			path := filepath.Join(config.Output.Path, id+".kepub.epub")
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("downloaded file missing or empty: %s", path)
			}
			return path
		}
	}
	t.Fatalf("bookmark %s not found in unread feed", id)
	return ""
}

// simulateRead inserts ReadStatus=2 for id into the Nickel DB.
func simulateRead(t *testing.T, dbPath, outputDir, id string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open nickel db: %v", err)
	}
	defer db.Close()
	contentID := fmt.Sprintf("file://%s/%s.kepub.epub", outputDir, id)
	if _, err = db.Exec(
		"INSERT INTO content (ContentID, ContentType, MimeType, ___UserID, ReadStatus) VALUES (?, ?, ?, ?, 2)",
		contentID, nickelContentTypeBook, "application/epub+zip", "test",
	); err != nil {
		t.Fatalf("insert read status: %v", err)
	}
}

// addToShelf inserts id into a named shelf in the Nickel DB.
func addToShelf(t *testing.T, dbPath, outputDir, id, shelfName string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open nickel db: %v", err)
	}
	defer db.Close()
	internalName := shelfName + "_internal"
	if _, err = db.Exec(
		"INSERT INTO Shelf (Id, InternalName, Name, _IsDeleted) VALUES (?, ?, ?, 'false')",
		shelfName+"_id", internalName, shelfName,
	); err != nil {
		t.Fatalf("insert shelf: %v", err)
	}
	contentID := fmt.Sprintf("file://%s/%s.kepub.epub", outputDir, id)
	if _, err = db.Exec(
		"INSERT INTO ShelfContent (ShelfName, ContentId, _IsDeleted) VALUES (?, ?, 'false')",
		internalName, contentID,
	); err != nil {
		t.Fatalf("insert shelf content: %v", err)
	}
}

// bookmarkAPIState returns the is_archived and is_marked state of a bookmark.
func bookmarkAPIState(t *testing.T, id string) (archived, marked bool) {
	t.Helper()
	resp := apiRequest(t, http.MethodGet, "/api/bookmarks/"+id, nil)
	var bm struct {
		IsArchived bool `json:"is_archived"`
		IsMarked   bool `json:"is_marked"`
	}
	json.NewDecoder(resp.Body).Decode(&bm)
	resp.Body.Close()
	return bm.IsArchived, bm.IsMarked
}

// TestSync exercises reconcileLocalFiles under different config combinations.
func TestSync(t *testing.T) {
	t.Run("archives completed book", func(t *testing.T) {
		id := createLoadedBookmark(t, testBookmarkURL)
		outputDir, dbPath := setupSyncEnv(t)
		config.Sync.Archive = true
		config.Output.Delete = true

		epubPath := downloadEntry(t, id)
		simulateRead(t, dbPath, outputDir, id)
		logOutput := captureLog(t)
		reconcileLocalFiles(&http.Client{Timeout: 30 * time.Second}, config, map[string]bool{id: true})
		logs := logOutput()

		archived, _ := bookmarkAPIState(t, id)
		if !archived {
			t.Error("bookmark should be archived after reading")
		}
		if _, err := os.Stat(epubPath); !os.IsNotExist(err) {
			t.Error("file should be deleted after archiving")
		}
		if !strings.Contains(logs, "marking entry "+id+" as archived") {
			t.Errorf("expected archive log message, got:\n%s", logs)
		}
		if !strings.Contains(logs, "deleted") {
			t.Errorf("expected delete log message, got:\n%s", logs)
		}
	})

	t.Run("skips archiving when disabled", func(t *testing.T) {
		id := createLoadedBookmark(t, testBookmarkURL)
		outputDir, dbPath := setupSyncEnv(t)
		config.Sync.Archive = false

		epubPath := downloadEntry(t, id)
		simulateRead(t, dbPath, outputDir, id)
		reconcileLocalFiles(&http.Client{Timeout: 30 * time.Second}, config, map[string]bool{id: true})

		archived, _ := bookmarkAPIState(t, id)
		if archived {
			t.Error("bookmark should not be archived when Archive=false")
		}
		if _, err := os.Stat(epubPath); err != nil {
			t.Error("file should still exist when Archive=false")
		}
	})

	t.Run("keeps file when delete disabled", func(t *testing.T) {
		id := createLoadedBookmark(t, testBookmarkURL)
		outputDir, dbPath := setupSyncEnv(t)
		config.Sync.Archive = true
		config.Output.Delete = false

		epubPath := downloadEntry(t, id)
		simulateRead(t, dbPath, outputDir, id)
		reconcileLocalFiles(&http.Client{Timeout: 30 * time.Second}, config, map[string]bool{id: true})

		archived, _ := bookmarkAPIState(t, id)
		if !archived {
			t.Error("bookmark should be archived")
		}
		if _, err := os.Stat(epubPath); err != nil {
			t.Error("file should still exist when Delete=false")
		}
	})

	t.Run("label filter excludes non-matching bookmark", func(t *testing.T) {
		id := createLoadedBookmark(t, testBookmarkURL)
		setupSyncEnv(t)
		config.Fetch.Labels = "nonexistentlabel"

		var buf bytes.Buffer
		if err := runCheck(&buf); err != nil {
			t.Fatalf("runCheck: %v", err)
		}
		if strings.Contains(buf.String(), id) {
			t.Errorf("bookmark %s should be excluded by label filter", id)
		}
	})

	t.Run("marks favourite from collection", func(t *testing.T) {
		id := createLoadedBookmark(t, testBookmarkURL)
		outputDir, dbPath := setupSyncEnv(t)
		config.Sync.FavouriteCollection = "MyFavourites"

		downloadEntry(t, id)
		addToShelf(t, dbPath, outputDir, id, "MyFavourites")
		reconcileLocalFiles(&http.Client{Timeout: 30 * time.Second}, config, map[string]bool{id: true})

		_, marked := bookmarkAPIState(t, id)
		if !marked {
			t.Error("bookmark should be marked as favourite")
		}
	})
}
