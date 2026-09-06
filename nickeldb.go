package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

type bookStatus int

// Derived from calibre's Kobo driver:
// https://github.com/kovidgoyal/calibre/blob/master/src/calibre/devices/kobo/driver.py
const (
	bookUnread  bookStatus = 0
	bookReading bookStatus = 1
	bookRead    bookStatus = 2
	bookClosed  bookStatus = 3
)

const nickelContentTypeBook = 6

type nickelDatabase struct {
	path    string
	verbose bool
}

func (db nickelDatabase) readStatus(id, outputDir string) (bookStatus, error) {
	conn, err := sql.Open("sqlite", "file:"+db.path+"?mode=ro")
	if err != nil {
		return bookUnread, fmt.Errorf("open Nickel DB: %w", err)
	}
	defer conn.Close()
	return nickelReadStatus(conn, id, outputDir, db.verbose)
}

func (db nickelDatabase) isInCollection(id, outputDir, collection string) (bool, error) {
	conn, err := sql.Open("sqlite", "file:"+db.path+"?mode=ro")
	if err != nil {
		return false, fmt.Errorf("open Nickel DB: %w", err)
	}
	defer conn.Close()
	return nickelIsInCollection(conn, id, outputDir, collection)
}

// nickelIsInCollection reports whether a book is in the named Kobo collection.
func nickelIsInCollection(db *sql.DB, id, outputDir, collection string) (bool, error) {
	contentID := nickelContentID(outputDir, id)
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM ShelfContent sc
		JOIN Shelf s ON sc.ShelfName = s.InternalName
		WHERE sc.ContentId = ? AND s.Name = ?
		  AND sc._IsDeleted = 'false' AND s._IsDeleted = 'false'`,
		contentID, collection).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// nickelReadStatus returns the current Nickel reading status for a book.
func nickelReadStatus(db *sql.DB, id, outputDir string, verbose bool) (bookStatus, error) {
	// Nickel stores books as file:// URIs matching the on-device path.
	path := nickelContentID(outputDir, id)
	row := db.QueryRow("SELECT ReadStatus FROM content WHERE ContentID = $1 AND ContentType = $2 LIMIT 1", path, nickelContentTypeBook)
	var status int
	if err := row.Scan(&status); err == sql.ErrNoRows {
		// Book not opened yet; Nickel hasn't created a row for it.
		return bookUnread, nil
	} else if err != nil {
		return bookUnread, err
	}
	debugf(verbose, "nickel book %s status: %d", id, status)
	switch bookStatus(status) {
	case bookUnread:
		return bookUnread, nil
	case bookReading:
		return bookReading, nil
	case bookRead:
		return bookRead, nil
	case bookClosed:
		return bookClosed, nil
	}
	// Unknown state — assume still reading so we don't delete a book in use.
	log.Printf("warning: unexpected Nickel book state: %d, assuming reading", status)
	return bookReading, nil
}

func nickelContentID(outputDir, id string) string {
	return fmt.Sprintf("file://%s/%s.kepub.epub", outputDir, id)
}
