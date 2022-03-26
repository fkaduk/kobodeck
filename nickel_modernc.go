//+build !sqlite3

package main

import (
	_ "modernc.org/sqlite"
)

const wallabakoSqliteBackend = "sqlite"
