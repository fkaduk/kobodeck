// Integration tests run against a real Readeck instance
// spun up in Docker via testcontainers-go.
// They check TODO

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
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

	config.URL = baseURL
	config.Token = token
	config.Limit = -1

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
	req, err := http.NewRequest(method, config.URL+path, body)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)
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

	var id string
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp = apiRequest(t, http.MethodGet, "/api/bookmarks", nil)
		var bookmarks []struct {
			ID     string `json:"id"`
			URL    string `json:"url"`
			Loaded bool   `json:"loaded"`
		}
		json.NewDecoder(resp.Body).Decode(&bookmarks)
		resp.Body.Close()
		for _, bm := range bookmarks {
			if bm.URL == bookmarkURL && bm.Loaded {
				id = bm.ID
				break
			}
		}
		if id != "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if id == "" {
		t.Fatalf("bookmark %s did not load within 30s", bookmarkURL)
	}
	t.Cleanup(func() {
		resp := apiRequest(t, http.MethodDelete, "/api/bookmarks/"+id, nil)
		resp.Body.Close()
	})
	return id
}

// createNickelDB creates a minimal Nickel-schema SQLite database in dir and
// returns its path. The caller can insert rows to simulate Kobo read status.
func createNickelDB(t *testing.T, dir string) string {
	t.Helper()
	dbPath := filepath.Join(dir, "KoboReader.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("create nickel db: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE content (
		ContentID   TEXT NOT NULL,
		ContentType TEXT NOT NULL,
		ReadStatus  INTEGER DEFAULT 0
	)`)
	if err != nil {
		t.Fatalf("create content table: %v", err)
	}
	return dbPath
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

// TestFullSync exercises the complete sync flow end-to-end:
// list → download → simulate read in Nickel DB → reconcileLocalFiles → verify archived + deleted.
func TestFullSync(t *testing.T) {
	id := createLoadedBookmark(t, testBookmarkURL)
	t.Logf("bookmark loaded: %s", id)

	outputDir := t.TempDir()
	dbPath := createNickelDB(t, t.TempDir())

	// Override config for this test, restore on cleanup.
	origOutput := config.Output
	origNickelDB := nickelDBPath
	origDelete := config.Delete
	t.Cleanup(func() {
		config.Output = origOutput
		nickelDBPath = origNickelDB
		config.Delete = origDelete
	})
	config.Output = outputDir
	nickelDBPath = dbPath
	config.Delete = true

	// 1. listBookmarks must include our bookmark.
	entries, err := listBookmarks()
	if err != nil {
		t.Fatalf("listBookmarks: %v", err)
	}
	var entry readeckBookmark
	for _, e := range entries {
		if e.ID == id {
			entry = e
			break
		}
	}
	if entry.ID == "" {
		t.Fatalf("bookmark %s not found in listBookmarks output", id)
	}

	// 2. download must produce a non-empty EPUB file.
	client := &http.Client{Timeout: 30 * time.Second}
	if err := download(client, entry); err != nil {
		t.Fatalf("download: %v", err)
	}
	epubPath := filepath.Join(outputDir, id+".epub")
	info, err := os.Stat(epubPath)
	if err != nil {
		t.Fatalf("epub not found after download: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("downloaded epub is empty")
	}
	t.Logf("downloaded %s (%d bytes)", epubPath, info.Size())

	// 3. Simulate reading: insert ReadStatus=2 into the Nickel DB.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open nickel db: %v", err)
	}
	contentID := fmt.Sprintf("file://%s/%s.epub", outputDir, id)
	_, err = db.Exec(
		"INSERT INTO content (ContentID, ContentType, ReadStatus) VALUES (?, ?, 2)",
		contentID, nickelContentTypeBook,
	)
	db.Close()
	if err != nil {
		t.Fatalf("insert read status: %v", err)
	}

	// 4. reconcileLocalFiles must mark the bookmark archived and delete the file.
	valid := map[string]bool{id: true}
	reconcileLocalFiles(config, valid)

	// 5. Verify archived in Readeck.
	resp := apiRequest(t, http.MethodGet, "/api/bookmarks/"+id, nil)
	var bm struct {
		IsArchived bool `json:"is_archived"`
	}
	json.NewDecoder(resp.Body).Decode(&bm)
	resp.Body.Close()
	if !bm.IsArchived {
		t.Error("bookmark should be archived after sync")
	}

	// 6. Verify file deleted from output dir.
	if _, err := os.Stat(epubPath); !os.IsNotExist(err) {
		t.Errorf("epub should have been deleted from %s", epubPath)
	}
}
