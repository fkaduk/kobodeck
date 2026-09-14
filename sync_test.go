package main

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDownloadRunPreservesAllBookmarkFailures(t *testing.T) {
	// Given
	firstErr := errors.New("first download failed")
	secondErr := errors.New("second download failed")
	var completed atomic.Int32
	run := newDownloadRun(2, 3, func(entry readeckBookmark) (bool, error) {
		completed.Add(1)
		switch entry.ID {
		case "first":
			return false, firstErr
		case "second":
			return false, secondErr
		default:
			return true, nil
		}
	})
	for _, id := range []string{"first", "successful", "second"} {
		run.schedule(readeckBookmark{ID: id})
	}

	// When
	filesChanged, err := run.finish()

	// Then
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("download error = %v, want both failures", err)
	}
	for _, bookmarkID := range []string{"first", "second"} {
		if !strings.Contains(err.Error(), "bookmark "+bookmarkID) {
			t.Errorf("download error does not identify bookmark %s: %v", bookmarkID, err)
		}
	}
	if got := completed.Load(); got != 3 {
		t.Fatalf("completed downloads = %d, want 3", got)
	}
	if !filesChanged {
		t.Fatal("successful download was not reported as a filesystem change")
	}
}
