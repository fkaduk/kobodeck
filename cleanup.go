package main

import (
	"errors"
	"io"
	"log"
	"os"
)

// closeWithWarning closes a resource and logs a non-fatal close error.
func closeWithWarning(name string, closer io.Closer) {
	if err := closer.Close(); err != nil {
		log.Printf("warning: close %s: %v", name, err)
	}
}

// removeWithWarning removes a path and ignores the already-absent case.
func removeWithWarning(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("warning: remove %s: %v", path, err)
	}
}
