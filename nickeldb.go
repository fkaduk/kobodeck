package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

type bookStatus int

const (
	bookUnread  bookStatus = 0
	bookReading bookStatus = 1
	bookRead    bookStatus = 2
)

const nickelContentTypeBook = 6

func openNickelDB() (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+nickelDBPath+"?mode=ro")
}

// nickelIsInCollection reports whether a book is in the named Kobo collection.
func nickelIsInCollection(db *sql.DB, ID, outputDir, collection string) (bool, error) {
	contentID := fmt.Sprintf("file://%s/%s.kepub.epub", outputDir, ID)
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM ShelfContent sc
		JOIN Shelf s ON sc.ShelfName = s.InternalName
		WHERE sc.ContentId = ? AND s.Name = ?`,
		contentID, collection).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func nickelReadStatus(db *sql.DB, ID string, outputDir string) (bookStatus, error) {
	// Nickel stores books as file:// URIs matching the on-device path.
	path := fmt.Sprintf("file://%s/%s.kepub.epub", outputDir, ID)
	row := db.QueryRow("SELECT ReadStatus FROM content WHERE ContentID = $1 AND ContentType = $2 LIMIT 1", path, nickelContentTypeBook)
	var status int
	if err := row.Scan(&status); err == sql.ErrNoRows {
		// Book not opened yet; Nickel hasn't created a row for it.
		return bookUnread, nil
	} else if err != nil {
		return bookUnread, err
	}
	debugf("nickel book %s status: %d", ID, status)
	// ReadStatus values: 0 = unread, 1 = in progress, 2 = finished.
	switch bookStatus(status) {
	case bookUnread:
		return bookUnread, nil
	case bookReading:
		return bookReading, nil
	case bookRead:
		return bookRead, nil
	}
	// Unknown state — assume still reading so we don't delete a book in use.
	log.Printf("warning: unexpected Nickel book state: %d, assuming reading", status)
	return bookReading, nil
}
