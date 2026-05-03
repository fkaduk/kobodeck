package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

func listEntries() ([]readeckBookmark, error) {
	client := &http.Client{Timeout: time.Duration(config.Timeout) * time.Second}
	var all []readeckBookmark
	page := 1
	const limit = 100
	for {
		url := fmt.Sprintf("%s/api/bookmarks?status=unread&limit=%d&page=%d",
			config.URL, limit, page)
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

		tp, err := strconv.Atoi(resp.Header.Get("Total-Pages"))
		if err != nil || page >= tp || (config.Limit > 0 && len(all) >= config.Limit) {
			break
		}
		page++
	}
	total := len(all)
	if config.Limit > 0 && len(all) > config.Limit {
		all = all[:config.Limit]
	}
	log.Printf("found %d unread bookmarks, will process %d", total, len(all))
	return all, nil
}

func checkTags(tags map[string]bool, labels []string) bool {
	for _, label := range labels {
		if tags[strings.ToLower(label)] {
			return true
		}
	}
	return false
}

func download(client *http.Client, entry readeckBookmark) error {
	if err := os.MkdirAll(config.Output, os.ModePerm); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	epubURL := config.URL + "/api/bookmarks/" + entry.ID + "/article.epub"
	output := filepath.Join(config.Output, entry.ID+".epub")

	info, err := os.Stat(output)
	if err == nil && info.ModTime().After(entry.Updated) && info.Size() > 0 {
		debugf("skipping %s: local file newer than bookmark (%s > %s)", output, info.ModTime(), entry.Updated)
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", output, err)
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
	return nil
}

func markAsRead(id string) error {
	log.Printf("marking entry %s as archived", id)
	body, _ := json.Marshal(map[string]bool{"is_archived": true})
	_, err := doAPI("PATCH", config.URL+"/api/bookmarks/"+id, bytes.NewBuffer(body))
	return err
}

func doAPI(method, apiURL string, body io.Reader) ([]byte, error) {
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
