package main

import (
	"errors"
	"io"
	"log"
	"os"
)

func closeWithWarning(name string, closer io.Closer) {
	if err := closer.Close(); err != nil {
		log.Printf("warning: close %s: %v", name, err)
	}
}

func removeWithWarning(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("warning: remove %s: %v", path, err)
	}
}
