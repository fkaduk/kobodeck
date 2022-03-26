/*

Parse read status from the Nickel UI database.

Nickel is Kobo's builtin and proprietary UI which stores book details
in a SQLite database.

We refer to it as "Nickel" here because that's the internal name used
by Kobo. This is to distinguish this UI from the Kobo *device* itself,
which also happens to run other UIs and programs like koreader, Plato,
etc.

The code simply reads the book status from the sqlite database.

*/
package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

// nickelNormalBook is the ContentID code for normal books in the Nickel sqlite database
const nickelNormalBook = 6

// the book status stored in the Nickel database
type nickelBookStatus int

// those happen to be incremental identifiers in the Nickel database,
// starting at zero
const (
	nickelBookUnread nickelBookStatus = iota
	nickelBookReading
	nickelBookRead
)

func readNickelStatus(ID int, outputDir string) (res bookStatus, err error) {
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
	rows, err := db.Query("SELECT ReadStatus FROM content WHERE ContentID = $1 AND ContentType = $2 LIMIT 1", path, nickelNormalBook)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	var readStatus nickelBookStatus
	if rows.Next() {
		if err = rows.Scan(&readStatus); err == nil {
			debugln("found Nickel readStatus", readStatus)
		} else {
			debugln("error scanning readstatus", err)
		}
	} else {
		err = rows.Err()
	}
	switch readStatus {
	case nickelBookUnread:
		return bookUnread, err
	case nickelBookReading:
		return bookReading, err
	case nickelBookRead:
		return bookRead, err
	}
	log.Printf("warning: unexpected Nickel book state: %d, assuming reading\n", readStatus)
	return bookReading, err
}
