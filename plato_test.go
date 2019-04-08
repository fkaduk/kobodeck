package main

import "testing"

func TestReadPlatoStatus(t *testing.T) {
	var err error
	meta, err = parsePlatoMetadata("metadata-plato-sample.json")
	if err != nil {
		t.Errorf("failure to setup test suite: %v", err)
	}
	res := checkPlatoStatus("wallabako/32827.epub")
	if !res {
		t.Errorf("Book status was incorrect, got: %v, want: %v", res, true)
	}
	res = checkPlatoStatus("wallabako/1.epub")
	if res {
		t.Errorf("Book status was incorrect, got: %v, want: %v", res, false)
	}
}
