package main

import (
	"io/ioutil"
	"os"
	"testing"
	"time"
)

// 2020-07-21 13:45:24 UTC
var mtime = time.Unix(1595339124, 0)

func createTempBook(t *testing.T, filePath string, data []byte) {
	err := ioutil.WriteFile(filePath, data, os.FileMode(0644))
	if err != nil {
		t.Fatalf("Unable to create/write file %s: %s", filePath, err)
	}

	err = os.Chtimes(filePath, mtime, mtime)
	if err != nil {
		t.Fatalf("Unable to change time on file %s: %s", filePath, err)
	}
}

func createTempReadingState(t *testing.T, filePath string, data []byte) {
	err := ioutil.WriteFile(filePath, data, 0644)

	if err != nil {
		t.Fatalf("Unable to create/write file %s: %s", filePath, err)
	}
}

// fingerprint is derived from mtime and size so we'll have to create a file, set the mtime, and check the fingerprint
func TestFingerprint(t *testing.T) {
	fat32EpochModTime = getFat32EpochModifiedTime("")
	tmpBookPath := "/tmp/fingerprint-test"

	defer os.Remove(tmpBookPath)
	createTempBook(t, tmpBookPath, []byte(mtime.String()))

	fingerprint, err := getFingerprint(tmpBookPath)
	if err != nil {
		t.Fatalf("Failed to get fingerprint: %s", err)
	}

	// This fingerprint has been checked against the migrate_metadata.py included in https://github.com/baskerville/plato/releases/tag/0.8.5
	expectedDecimal := "5496726306793979933"
	expectedHex := "4C484B740000001D"

	if fingerprint.String() != expectedDecimal {
		t.Errorf("Fingerprint decimal is incorrect, got: %v, want: %v", fingerprint.String(), expectedDecimal)
	}

	if fingerprint.Hex() != expectedHex {
		t.Errorf("Fingerprint hex is incorrect, got: %v, want: %v", fingerprint.Hex(), expectedHex)
	}
}

func TestReadPlatoStatus(t *testing.T) {
	var err error

	tmpBookDir := "/tmp/wallabako-books/"

	defer os.RemoveAll(tmpBookDir)
	err = os.Mkdir(tmpBookDir, 0755)
	if err != nil {
		t.Fatalf("Unable to create temp directory for test: %s", err)
	}

	tmpBookPath := tmpBookDir + "32827.epub"
	createTempBook(t, tmpBookPath, []byte("foo"))
	tmpBookStatePath := tmpBookDir + "4C484B7400000003.json"
	createTempReadingState(t, tmpBookStatePath, []byte(`{
      "opened": "2019-04-06 23:21:22",
      "currentPage": 181623,
      "pagesCount": 181834,
      "finished": true,
      "rotation": 3,
      "fontSize": 10.660494
    }`))

	tmpBookPath2 := tmpBookDir + "35130.epub"
	createTempBook(t, tmpBookPath2, []byte("foobar"))
	tmpBookStatePath2 := tmpBookDir + "4C484B7400000006.json"
	createTempReadingState(t, tmpBookStatePath2, []byte(`{
      "opened": "2019-04-06 23:21:22",
      "currentPage": 1,
      "pagesCount": 181834,
      "finished": false,
      "rotation": 3,
      "fontSize": 10.660494
    }`))

	tmpBookPath3 := tmpBookDir + "1.epub"
	createTempBook(t, tmpBookPath3, []byte("foobarbaz"))

	meta, err = parsePlatoMetadata("metadata-plato-sample-legacy.json", tmpBookDir)
	if err == nil {
		t.Fatalf("legacy .metadata.json should fail to unmarshal")
	}

	meta, err = parsePlatoMetadata("metadata-plato-sample.json", tmpBookDir)
	if err != nil {
		t.Fatalf("failure to setup test suite: %v", err)
	}

	res := checkPlatoStatus(tmpBookPath)
	if res != bookRead {
		t.Errorf("Book status was incorrect, got: %v, want: %v (read)", res, bookRead)
	}
	res = checkPlatoStatus(tmpBookPath2)
	if res != bookReading {
		t.Errorf("Book status was incorrect, got: %v, want: %v (reading)", res, bookReading)
	}
	res = checkPlatoStatus(tmpBookPath3)
	if res != bookUnread {
		t.Errorf("Book status was incorrect, got: %v, want: %v (unread)", res, bookUnread)
	}
}

func TestReadLegacyPlatoStatus(t *testing.T) {
	var err error
	legacyMeta, err = parsePlatoLegacyMetadata("metadata-plato-sample-legacy.json")
	if err != nil {
		t.Fatalf("failure to setup test suite: %v", err)
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
