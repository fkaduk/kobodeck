package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pgaskin/kepubify/v4/kepub"
)

type readeckBookmark struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	Updated    time.Time `json:"updated"`
	IsArchived bool      `json:"is_archived"`
	Labels     []string  `json:"labels"`
	Loaded     bool      `json:"loaded"`
}

// listBookmarks fetches all unread bookmarks from Readeck, paging through results
// in batches. Stops early if config.Limit is reached.
func listBookmarks() ([]readeckBookmark, error) {
	client := &http.Client{Timeout: time.Duration(config.Timeout) * time.Second}
	var all []readeckBookmark
	const batchSize = 100
	for offset := 0; ; offset += batchSize {
		url := fmt.Sprintf("%s/api/bookmarks?is_archived=false&limit=%d&offset=%d",
			config.URL, batchSize, offset)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("build list request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+config.Token)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list bookmarks: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("list bookmarks: unexpected status %s", resp.Status)
		}

		var pageItems []readeckBookmark
		if err = json.NewDecoder(resp.Body).Decode(&pageItems); err != nil {
			return nil, fmt.Errorf("decode bookmarks: %w", err)
		}
		all = append(all, pageItems...)

		if len(pageItems) < batchSize || (config.Limit > 0 && len(all) >= config.Limit) {
			break
		}
	}
	total := len(all)
	if config.Limit > 0 && len(all) > config.Limit {
		all = all[:config.Limit]
	}
	log.Printf("found %d unread bookmarks, will process %d", total, len(all))
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

// download fetches the EPUB for a bookmark and writes it to config.Output.
// Skips the download if a local file newer than the bookmark's updated timestamp already exists.
// Deletes the partial file if the write fails.
func download(client *http.Client, entry readeckBookmark) error {
	if err := os.MkdirAll(config.Output, os.ModePerm); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	epubURL := config.URL + "/api/bookmarks/" + entry.ID + "/article.epub"
	output := filepath.Join(config.Output, entry.ID+".epub")

	checkPath := output
	if config.Kepub {
		checkPath = filepath.Join(config.Output, entry.ID+".kepub.epub")
	}
	info, err := os.Stat(checkPath)
	if err == nil && info.ModTime().After(entry.Updated) && info.Size() > 0 {
		debugf("skipping %s: local file newer than bookmark (%s > %s)", checkPath, info.ModTime(), entry.Updated)
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", checkPath, err)
	}

	log.Printf("downloading %s to %s", epubURL, output)
	req, err := http.NewRequest("GET", epubURL, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)

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
	defer out.Close()

	n, err := io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(output)
		return fmt.Errorf("write %s: %w", output, err)
	}
	filesChanged.Store(true)
	log.Printf("wrote %s (%d bytes) timestamp %s", output, n, entry.Updated)

	if config.Covers {
		if err := fixCover(output); err != nil {
			log.Printf("warning: cover fix %s: %v", filepath.Base(output), err)
		}
	}

	if config.Kepub {
		kepubPath, err := toKepub(output)
		if err != nil {
			return fmt.Errorf("kepub convert %s: %w", output, err)
		}
		log.Printf("converted to %s", kepubPath)
	}
	return nil
}

// toKepub converts the EPUB at path to a .kepub.epub file, removes the
// original, and returns the new path.
func toKepub(epubPath string) (string, error) {
	r, err := zip.OpenReader(epubPath)
	if err != nil {
		return "", err
	}
	defer r.Close()

	kepubPath := strings.TrimSuffix(epubPath, ".epub") + ".kepub.epub"
	f, err := os.Create(kepubPath)
	if err != nil {
		return "", err
	}

	c := kepub.NewConverterWithOptions(kepub.ConverterOptionDummyTitlepage(false))
	if err := c.Convert(context.Background(), f, &r.Reader); err != nil {
		f.Close()
		os.Remove(kepubPath)
		return "", err
	}
	f.Close()
	os.Remove(epubPath)
	return kepubPath, nil
}

// archiveBookmark archives a bookmark in Readeck, removing it from the unread feed.
func archiveBookmark(id string) error {
	log.Printf("marking entry %s as archived", id)
	body, _ := json.Marshal(map[string]bool{"is_archived": true})
	_, err := callAPI("PATCH", config.URL+"/api/bookmarks/"+id, bytes.NewBuffer(body))
	return err
}

// callAPI sends an authenticated API request and returns the response body.
// Returns an error if the status code is outside the 2xx range.
func callAPI(method, apiURL string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, apiURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)
	req.Header.Set("Content-Type", "application/json")
	if config.Verbose {
		dump, _ := httputil.DumpRequestOut(req, true)
		debugf("request: %q", dump)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if config.Verbose {
		dump, _ := httputil.DumpResponse(resp, true)
		debugf("response: %q", dump)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, fmt.Errorf("API %s %s: %s", method, apiURL, resp.Status)
	}
	return data, nil
}
