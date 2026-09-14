package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type bookmarkStore interface {
	getBookmark(id string) (readeckBookmark, error)
	patchBookmark(id string, fields map[string]any) error
}

type nickelLibrary interface {
	readStatus(id, outputDir string) (bookStatus, error)
	isInCollection(id, outputDir, collection string) (bool, error)
}

type localBook struct {
	id   string
	path string
}

type readeckUpdateResult struct {
	koboReadingStatus bookStatus
	archivedInReadeck bool
}

func listLocalBooks(outputDir string) ([]localBook, error) {
	files, err := filepath.Glob(strings.TrimSuffix(outputDir, "/") + "/*.kepub.epub")
	if err != nil {
		return nil, err
	}
	books := make([]localBook, 0, len(files))
	for _, file := range files {
		books = append(books, localBook{
			id:   strings.TrimSuffix(filepath.Base(file), ".kepub.epub"),
			path: file,
		})
	}
	return books, nil
}

// syncLocalBooks updates Readeck from Kobo state, then removes eligible local
// books only after all updates for that book have succeeded.
func syncLocalBooks(
	readeck bookmarkStore,
	nickel nickelLibrary,
	cfg appConfig,
	keepLocal map[string]bool,
	bookmarks map[string]readeckBookmark,
	allowDelete bool,
) (bool, error) {
	outputDir := strings.TrimSuffix(cfg.Output.Path, "/")
	books, err := listLocalBooks(outputDir)
	if err != nil {
		return false, fmt.Errorf("cannot list local books: %w", err)
	}
	debugf(cfg.Log.Verbose, "local books to inspect: %v", books)

	var syncErr error
	filesChanged := false
	for _, book := range books {
		if book.id == "" {
			log.Println("skipping file with empty name:", book.path)
			continue
		}
		result, err := updateReadeckFromKobo(readeck, nickel, cfg, outputDir, book, bookmarks)
		if err == nil {
			keep := keepLocal[book.id] && !result.archivedInReadeck
			changed, removeErr := removeStaleLocalBook(book, keep, allowDelete, result.koboReadingStatus)
			if changed {
				filesChanged = true
			}
			err = removeErr
		}
		if err != nil {
			log.Printf("warning: failed to sync local book %s: %s", book.path, err)
			syncErr = errors.Join(syncErr, fmt.Errorf("%s: %w", book.path, err))
		}
	}
	return filesChanged, syncErr
}

func updateReadeckFromKobo(
	readeck bookmarkStore,
	nickel nickelLibrary,
	cfg appConfig,
	outputDir string,
	book localBook,
	bookmarks map[string]readeckBookmark,
) (readeckUpdateResult, error) {
	result := readeckUpdateResult{}
	status, statusErr := nickel.readStatus(book.id, outputDir)
	result.koboReadingStatus = status

	var updateErr error
	var inCollection bool
	collectionKnown := cfg.Sync.FavouriteCollection == ""
	if cfg.Sync.FavouriteCollection != "" {
		var err error
		inCollection, err = nickel.isInCollection(book.id, outputDir, cfg.Sync.FavouriteCollection)
		if err != nil {
			log.Println("failed to check collection:", err)
			updateErr = errors.Join(updateErr, fmt.Errorf("check collection: %w", err))
		} else {
			collectionKnown = true
		}
	}
	if statusErr != nil {
		// Unknown reading status must preserve the local book.
		log.Println(statusErr)
		return result, errors.Join(updateErr, fmt.Errorf("read status: %w", statusErr))
	}

	bookmark, bookmarkKnown := bookmarks[book.id]
	if status == bookRead && !bookmarkKnown {
		var err error
		bookmark, err = readeck.getBookmark(book.id)
		if err != nil {
			log.Printf("cannot read Readeck bookmark %s: %v", book.id, err)
			updateErr = errors.Join(updateErr, fmt.Errorf("get bookmark: %w", err))
		} else {
			bookmarkKnown = true
		}
	}
	if status == bookRead && bookmarkKnown {
		fields := make(map[string]any)
		action := "read"
		if bookmark.ReadProgress != 100 {
			fields["read_progress"] = 100
		}
		if cfg.Sync.Archive {
			fields["is_archived"] = true
			action += " and archived"
		}
		if len(fields) > 0 {
			log.Printf("marking entry %s as %s", book.id, action)
			if err := readeck.patchBookmark(book.id, fields); err != nil {
				log.Printf("failed to mark entry %s as %s: %v", book.id, action, err)
				updateErr = errors.Join(updateErr, fmt.Errorf("mark read: %w", err))
			} else if cfg.Sync.Archive {
				result.archivedInReadeck = true
			}
		}
	}
	if collectionKnown && cfg.Sync.FavouriteCollection != "" && !bookmarkKnown {
		var err error
		bookmark, err = readeck.getBookmark(book.id)
		if err != nil {
			log.Printf("cannot read Readeck favourite state for %s: %v", book.id, err)
			updateErr = errors.Join(updateErr, fmt.Errorf("get favourite state: %w", err))
		} else {
			bookmarkKnown = true
		}
	}
	if collectionKnown && bookmarkKnown && cfg.Sync.FavouriteCollection != "" && inCollection != bookmark.IsMarked {
		action := "marking"
		if !inCollection {
			action = "unmarking"
		}
		log.Printf("%s entry %s as favourite", action, book.id)
		if err := readeck.patchBookmark(book.id, map[string]any{"is_marked": inCollection}); err != nil {
			log.Printf("failed to set favourite state to %t: %v", inCollection, err)
			updateErr = errors.Join(updateErr, fmt.Errorf("set favourite state: %w", err))
		}
	}
	return result, updateErr
}

func removeStaleLocalBook(book localBook, keepLocal, allowDelete bool, status bookStatus) (bool, error) {
	if !allowDelete || keepLocal {
		return false, nil
	}
	if status == bookReading || status == bookClosed {
		log.Printf("not deleting book currently being read: %s", book.path)
		return false, nil
	}
	if err := os.Remove(book.path); err != nil {
		return false, fmt.Errorf("delete book: %w", err)
	}
	log.Println("deleted", book.path)
	return true, nil
}
