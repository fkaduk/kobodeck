package main

import "testing"

func TestReadPlatoStatus(t *testing.T) {
	var err error
	meta, err = parsePlatoMetadata("metadata-plato-sample.json")
	if err != nil {
		t.Errorf("failure to setup test suite: %v", err)
	}
	res := checkPlatoStatus("wallabako/32827.epub")
	if res != bookRead {
		t.Errorf("Book status was incorrect, got: %v, want: %v (read)", res, bookRead)
	}
	res = checkPlatoStatus("wallabako/35130.epub")
	if res != bookReading {
		t.Errorf("Book status was incorrect, got: %v, want: %v (reading)", res, bookReading)
	}
	res = checkPlatoStatus("wallabako/1.epub")
	if res != bookUnread {
		t.Errorf("Book status was incorrect, got: %v, want: %v (unread)", res, bookUnread)
	}
}
