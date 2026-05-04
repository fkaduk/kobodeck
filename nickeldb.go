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

func readStatus(ID string, outputDir string) (bookStatus, error) {
	// KoboReader.sqlite is Nickel's main database; open read-write is fine, we never write.
	db, err := sql.Open("sqlite", nickelDBPath)
	if err != nil {
		return bookUnread, err
	}
	defer db.Close()

	// Nickel stores books as file:// URIs matching the on-device path
	path := fmt.Sprintf("file://%s/%s.epub", outputDir, ID)
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
