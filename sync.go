package main

import (
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

type downloadRun struct {
	group        errgroup.Group
	download     func(readeckBookmark) (bool, error)
	filesChanged atomic.Bool
	failures     chan error
}

func newDownloadRun(workerLimit, maxDownloads int, download func(readeckBookmark) (bool, error)) *downloadRun {
	run := &downloadRun{
		download: download,
		failures: make(chan error, maxDownloads),
	}
	run.group.SetLimit(workerLimit)
	return run
}

// schedule adds a bookmark download to the run. Failures are collected so
// the errgroup controls concurrency and completion without losing later errors.
func (run *downloadRun) schedule(entry readeckBookmark) {
	run.group.Go(func() error {
		changed, err := run.download(entry)
		if changed {
			run.filesChanged.Store(true)
		}
		if err != nil {
			run.failures <- fmt.Errorf("bookmark %s: %w", entry.ID, err)
		}
		return nil
	})
}

// finish waits for scheduled downloads and collects their results. It may only
// be called once after all downloads have been scheduled.
func (run *downloadRun) finish() (bool, error) {
	waitErr := run.group.Wait()
	close(run.failures)
	var failures []error
	for err := range run.failures {
		failures = append(failures, err)
	}
	return run.filesChanged.Load(), errors.Join(errors.Join(failures...), waitErr)
}

func (a app) sync() error {
	log.Println("connecting to", a.cfg.Server.URL)
	time.Sleep(5 * time.Second)
	entries, err := a.readeck.listBookmarks(a.cfg.Fetch)
	for attempt := 1; err != nil && attempt < 5; attempt++ {
		delay := time.Duration(1<<uint(attempt)) * time.Second
		log.Printf("failed to connect, retrying in %s: %v", delay, err)
		time.Sleep(delay)
		entries, err = a.readeck.listBookmarks(a.cfg.Fetch)
	}
	if err != nil {
		return err
	}

	matchedEntries, _ := filterBookmarksByLabel(entries, a.cfg.Fetch.Labels)
	keepLocal := make(map[string]bool)
	bookmarks := make(map[string]readeckBookmark, len(entries))
	for _, entry := range entries {
		bookmarks[entry.ID] = entry
	}
	for _, entry := range matchedEntries {
		keepLocal[entry.ID] = true
	}

	downloads := newDownloadRun(a.cfg.Fetch.Workers, len(entries), func(entry readeckBookmark) (bool, error) {
		return a.readeck.downloadBookmarkFile(a.cfg.Output, entry)
	})
	filesChanged := false

	for _, entry := range entries {
		if !keepLocal[entry.ID] {
			debugf(a.cfg.Log.Verbose, "skipping %s (not in tags)", entry.ID)
			continue
		}
		debugf(a.cfg.Log.Verbose, "dispatching %s", entry.ID)
		downloads.schedule(entry)
	}
	var syncErr error
	downloadsChanged, err := downloads.finish()
	if err != nil {
		log.Println("download error:", err)
		syncErr = errors.Join(syncErr, fmt.Errorf("downloads failed: %w", err))
	}
	if downloadsChanged {
		filesChanged = true
	}
	changed, err := syncLocalBooks(a.readeck, a.nickel, a.cfg, keepLocal, bookmarks, a.cfg.Output.Delete)
	if err != nil {
		log.Println("local book sync error:", err)
		syncErr = errors.Join(syncErr, err)
	}
	if changed {
		filesChanged = true
	}

	if filesChanged {
		if err := nickelRescan(a.nickelStatusPath); err != nil {
			log.Printf("nickel rescan failed: %v", err)
			syncErr = errors.Join(syncErr, fmt.Errorf("nickel rescan failed: %w", err))
		}
	}
	return syncErr
}
