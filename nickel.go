package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

const wallabakoSqliteBackend = "sqlite"

const nickelNormalBook = 6

type nickelBookStatus int

const (
	nickelBookUnread nickelBookStatus = iota
	nickelBookReading
	nickelBookRead
)

func readNickelStatus(ID string, outputDir string) (res bookStatus, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Println("failed to read Nickel status:", r)
		}
	}()
	if len(config.Database) <= 0 {
		return res, fmt.Errorf("no database configured")
	}
	db, err := sql.Open(wallabakoSqliteBackend, config.Database)
	if err != nil {
		return res, err
	}
	defer db.Close()

	path := fmt.Sprintf("file://%s/%s.epub", outputDir, ID)
	rows, err := db.Query("SELECT ReadStatus FROM content WHERE ContentID = $1 AND ContentType = $2 LIMIT 1", path, nickelNormalBook)
	if err != nil {
		return res, err
	}
	defer rows.Close()

	var readStatus nickelBookStatus
	if rows.Next() {
		if err = rows.Scan(&readStatus); err != nil {
			debugln("error scanning readstatus", err)
		} else {
			debugln("found Nickel readStatus", readStatus)
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
