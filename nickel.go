package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

// koboNormalBook is the ContentID code for normal books in the Kobo sqlite database
const koboNormalBook = 6

// the book status stored in the Kobo database
type koboBookStatus int

// those happen to be incremental identifiers in the Kobo database,
// starting at zero
const (
	koboBookUnread koboBookStatus = iota
	koboBookReading
	koboBookRead
)

func readKoboStatus(ID int, outputDir string) (res bookStatus, err error) {
	if len(config.Database) <= 0 {
		return res, fmt.Errorf("no database configured")
	}
	// XXX: this should be a singleton if we start calling readStatus
	// more often
	db, err := sql.Open("sqlite3", config.Database)
	if err != nil {
		return res, err
	}
	defer db.Close()

	path := fmt.Sprintf("file://%s/%d.epub", outputDir, ID)
	rows, err := db.Query("SELECT ReadStatus FROM content WHERE ContentID = $1 AND ContentType = $2 LIMIT 1", path, koboNormalBook)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	var readStatus koboBookStatus
	if rows.Next() {
		if err = rows.Scan(&readStatus); err == nil {
			debugln("found Kobo readStatus", readStatus)
		} else {
			debugln("error scanning readstatus", err)
		}
	} else {
		err = rows.Err()
	}
	switch readStatus {
	case koboBookUnread:
		return bookUnread, err
	case koboBookReading:
		return bookReading, err
	case koboBookRead:
		return bookRead, err
	}
	log.Printf("warning: unexpected Kobo book state: %d, assuming reading\n", readStatus)
	return bookReading, err
}
