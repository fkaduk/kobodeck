package main

import (
	"testing"
)

func TestReadKoreaderStatus(t *testing.T) {

	checklist := map[string]bookStatus{
		"metadata-koreader-reading.txt.lua":  bookReading,
		"metadata-koreader-complete.txt.lua": bookRead,
		"metadata-koreader-100.txt.lua":      bookRead,
	}
	for path, status := range checklist {
		res, err := parseKoreaderStatus(path)
		if err != nil {
			t.Fatalf("failed to parse known good file %s: %s", path, err)
		}
		if res != status {
			t.Errorf("metadata %s should have been %d, was %d", path, status, res)
		}
	}
}
