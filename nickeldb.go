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

const nickelNormalBook = 6

func readStatus(ID string, outputDir string) (bookStatus, error) {
	db, err := sql.Open("sqlite", nickelDB)
	if err != nil {
		return bookUnread, err
	}
	defer db.Close()

	path := fmt.Sprintf("file://%s/%s.epub", outputDir, ID)
	row := db.QueryRow("SELECT ReadStatus FROM content WHERE ContentID = $1 AND ContentType = $2 LIMIT 1", path, nickelNormalBook)
	var status int
	if err := row.Scan(&status); err == sql.ErrNoRows {
		return bookUnread, nil
	} else if err != nil {
		return bookUnread, err
	}
	debugf("nickel book %s status: %d", ID, status)
	switch bookStatus(status) {
	case bookUnread:
		return bookUnread, nil
	case bookReading:
		return bookReading, nil
	case bookRead:
		return bookRead, nil
	}
	log.Printf("warning: unexpected Nickel book state: %d, assuming reading", status)
	return bookReading, nil
}
