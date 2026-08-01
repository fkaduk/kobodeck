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

// listBookmarks fetches bookmarks from Readeck, paging through results
// in batches. Stops early if config.Limit is reached.
func listBookmarks(client *http.Client) ([]readeckBookmark, error) {
	var all []readeckBookmark
	const batchSize = 100
	for offset := 0; ; offset += batchSize {
		u, err := url.Parse(config.Server.URL + "/api/bookmarks")
		if err != nil {
			return nil, fmt.Errorf("build bookmark list URL: %w", err)
		}
		query := u.Query()
		query.Set("is_archived", "false")
		query.Set("limit", strconv.Itoa(batchSize))
		query.Set("offset", strconv.Itoa(offset))
		for _, s := range strings.Split(config.Fetch.Status, ",") {
			if s = strings.TrimSpace(s); s != "" {
				query.Add("read_status", s)
			}
		}
		u.RawQuery = query.Encode()

		data, err := doAPIRequest(client, "GET", u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("list bookmarks: %w", err)
		}

		var pageItems []readeckBookmark
		if err := json.Unmarshal(data, &pageItems); err != nil {
			return nil, fmt.Errorf("decode bookmarks: %w", err)
		}
		all = append(all, pageItems...)

		if len(pageItems) < batchSize || (config.Fetch.Limit > 0 && len(all) >= config.Fetch.Limit) {
			break
		}
	}
	total := len(all)
	if config.Fetch.Limit > 0 && len(all) > config.Fetch.Limit {
		all = all[:config.Fetch.Limit]
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

// download fetches the EPUB for a bookmark and writes it to config.Output.Path.
// Skips the download if the kepub file already exists and is non-empty.
// Deletes the partial file if the write fails.
// TODO: rename to downloadBookmarkFile
func download(client *http.Client, entry readeckBookmark) error {
	if err := os.MkdirAll(config.Output.Path, os.ModePerm); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	epubURL := config.Server.URL + "/api/bookmarks/" + entry.ID + "/article.epub"
	output := filepath.Join(config.Output.Path, entry.ID+".epub")

	checkPath := filepath.Join(config.Output.Path, entry.ID+".kepub.epub")
	info, err := os.Stat(checkPath)
	if err == nil && info.Size() > 0 {
		debugf("skipping %s: already downloaded", checkPath)
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", checkPath, err)
	}

	log.Printf("downloading %s to %s", epubURL, output)
	req, err := http.NewRequest("GET", epubURL, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.Server.Token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", epubURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", epubURL, resp.Status)
	}

	out, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}

	n, writeErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(output)
		if writeErr != nil {
			return fmt.Errorf("write %s: %w", output, writeErr)
		}
		return fmt.Errorf("close %s: %w", output, closeErr)
	}
	log.Printf("wrote %s (%d bytes)", output, n)

	if err := fixCover(output); err != nil {
		log.Printf("warning: cover fix %s: %v", filepath.Base(output), err)
	}

	kepubPath, err := toKepub(output)
	if err != nil {
		return fmt.Errorf("kepub convert %s: %w", output, err)
	}
	// Set mtime to the article's update time so Nickel sorts by article date,
	// not download time. Both fixCover and toKepub create new files, so this
	// must happen after conversion.
	if err := os.Chtimes(kepubPath, entry.Updated, entry.Updated); err != nil {
		log.Printf("warning: set mtime %s: %v", filepath.Base(kepubPath), err)
	}
	filesChanged.Store(true)
	log.Printf("converted to %s (timestamp %s)", kepubPath, entry.Updated.Format(time.RFC3339))
	return nil
}

// patchBookmark sends a partial update to a bookmark in Readeck.
func patchBookmark(client *http.Client, id string, fields map[string]any) error {
	body, _ := json.Marshal(fields)
	_, err := doAPIRequest(client, "PATCH", config.Server.URL+"/api/bookmarks/"+id, bytes.NewBuffer(body))
	return err
}

// getBookmark retrieves the metadata of a single bookmark
func getBookmark(client *http.Client, id string) (readeckBookmark, error) {
	data, err := doAPIRequest(client, "GET", config.Server.URL+"/api/bookmarks/"+id, nil)
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
func doAPIRequest(client *http.Client, method, apiURL string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, apiURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+config.Server.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		debugf("http method=%s path=%s outcome=transport_failure duration=%s",
			method, req.URL.Path, time.Since(start).Truncate(time.Millisecond))
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	debugf("http method=%s path=%s status=%d bytes=%d duration=%s",
		method, req.URL.Path, resp.StatusCode, len(data), time.Since(start).Truncate(time.Millisecond))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, fmt.Errorf("API %s %s: %s", method, apiURL, resp.Status)
	}
	return data, nil
}
