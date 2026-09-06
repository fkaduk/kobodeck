package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type readeckBookmark struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	URL          string    `json:"url"`
	Updated      time.Time `json:"updated"`
	ReadProgress int       `json:"read_progress"`
	IsArchived   bool      `json:"is_archived"`
	IsMarked     bool      `json:"is_marked"` // "marked" is what Readeck calls a favorite.
	Labels       []string  `json:"labels"`
	Loaded       bool      `json:"loaded"`
}

type readeckClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
	verbose    bool
}

func newReadeckClient(httpClient *http.Client, server serverConfig, verbose bool) readeckClient {
	return readeckClient{
		httpClient: httpClient,
		baseURL:    server.URL,
		token:      server.Token,
		verbose:    verbose,
	}
}

func (client readeckClient) debugf(format string, args ...interface{}) {
	if client.verbose {
		log.Printf(format, args...)
	}
}

// listBookmarks fetches bookmarks from Readeck, paging through results
// in batches. Stops early if fetch.Limit is reached.
func (client readeckClient) listBookmarks(fetch fetchConfig) ([]readeckBookmark, error) {
	var all []readeckBookmark
	const batchSize = 100
	for offset := 0; ; offset += batchSize {
		u, err := url.Parse(client.baseURL + "/api/bookmarks")
		if err != nil {
			return nil, fmt.Errorf("build bookmark list URL: %w", err)
		}
		query := u.Query()
		query.Set("is_archived", "false")
		query.Set("limit", strconv.Itoa(batchSize))
		query.Set("offset", strconv.Itoa(offset))
		for _, s := range strings.Split(fetch.Status, ",") {
			if s = strings.TrimSpace(s); s != "" {
				query.Add("read_status", s)
			}
		}
		u.RawQuery = query.Encode()

		data, err := client.doAPIRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("list bookmarks: %w", err)
		}

		var pageItems []readeckBookmark
		if err := json.Unmarshal(data, &pageItems); err != nil {
			return nil, fmt.Errorf("decode bookmarks: %w", err)
		}
		all = append(all, pageItems...)

		if len(pageItems) < batchSize || (fetch.Limit > 0 && len(all) >= fetch.Limit) {
			break
		}
	}
	total := len(all)
	if fetch.Limit > 0 && len(all) > fetch.Limit {
		all = all[:fetch.Limit]
	}
	log.Printf("found %d bookmarks, will process %d", total, len(all))
	return all, nil
}

// matchesLabelFilter reports whether any of the bookmark's labels match the tag filter.
func matchesLabelFilter(tags map[string]bool, labels []string) bool {
	for _, label := range labels {
		if tags[strings.ToLower(label)] {
			return true
		}
	}
	return false
}

// downloadBookmarkFile fetches the EPUB for a bookmark and writes it to outputCfg.Path.
// Skips the download if the kepub file already exists and is non-empty.
// Deletes the partial file if the write fails.
func (client readeckClient) downloadBookmarkFile(outputCfg outputConfig, entry readeckBookmark) (bool, error) {
	if err := os.MkdirAll(outputCfg.Path, os.ModePerm); err != nil {
		return false, fmt.Errorf("create output dir: %w", err)
	}
	epubURL := client.baseURL + "/api/bookmarks/" + entry.ID + "/article.epub"
	output := filepath.Join(outputCfg.Path, entry.ID+".epub")

	checkPath := filepath.Join(outputCfg.Path, entry.ID+".kepub.epub")
	info, err := os.Stat(checkPath)
	if err == nil && info.Size() > 0 {
		client.debugf("skipping %s: already downloaded", checkPath)
		return false, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", checkPath, err)
	}

	log.Printf("downloading %s to %s", epubURL, output)
	req, err := http.NewRequest(http.MethodGet, epubURL, nil)
	if err != nil {
		return false, fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+client.token)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("download %s: %w", epubURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("download %s: %s", epubURL, resp.Status)
	}

	out, err := os.Create(output)
	if err != nil {
		return false, fmt.Errorf("create %s: %w", output, err)
	}

	n, writeErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(output)
		if writeErr != nil {
			return false, fmt.Errorf("write %s: %w", output, writeErr)
		}
		return false, fmt.Errorf("close %s: %w", output, closeErr)
	}
	log.Printf("wrote %s (%d bytes)", output, n)

	if err := fixCover(output); err != nil {
		log.Printf("warning: cover fix %s: %v", filepath.Base(output), err)
	}

	kepubPath, err := toKepub(output)
	if err != nil {
		return false, fmt.Errorf("kepub convert %s: %w", output, err)
	}
	// Set mtime to the article's update time so Nickel sorts by article date,
	// not download time. Both fixCover and toKepub create new files, so this
	// must happen after conversion.
	if err := os.Chtimes(kepubPath, entry.Updated, entry.Updated); err != nil {
		log.Printf("warning: set mtime %s: %v", filepath.Base(kepubPath), err)
	}
	log.Printf("converted to %s (timestamp %s)", kepubPath, entry.Updated.Format(time.RFC3339))
	return true, nil
}

// patchBookmark sends a partial update to a bookmark in Readeck.
func (client readeckClient) patchBookmark(id string, fields map[string]any) error {
	body, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("encode bookmark patch: %w", err)
	}
	_, err = client.doAPIRequest(http.MethodPatch, client.baseURL+"/api/bookmarks/"+id, bytes.NewBuffer(body))
	return err
}

// getBookmark retrieves the metadata of a single bookmark.
func (client readeckClient) getBookmark(id string) (readeckBookmark, error) {
	data, err := client.doAPIRequest(http.MethodGet, client.baseURL+"/api/bookmarks/"+id, nil)
	if err != nil {
		return readeckBookmark{}, err
	}
	var bookmark readeckBookmark
	if err := json.Unmarshal(data, &bookmark); err != nil {
		return readeckBookmark{}, err
	}
	return bookmark, nil
}

// doAPIRequest sends an authenticated API request and returns the response body.
// Returns an error if the status code is outside the 2xx range.
func (client readeckClient) doAPIRequest(method, apiURL string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, apiURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+client.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := client.httpClient.Do(req)
	if err != nil {
		client.debugf("http method=%s path=%s outcome=transport_failure duration=%s",
			method, req.URL.Path, time.Since(start).Truncate(time.Millisecond))
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	client.debugf("http method=%s path=%s status=%d bytes=%d duration=%s",
		method, req.URL.Path, resp.StatusCode, len(data), time.Since(start).Truncate(time.Millisecond))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, fmt.Errorf("API %s %s: %s", method, apiURL, resp.Status)
	}
	return data, nil
}
