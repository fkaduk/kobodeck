package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

const metadataPath = "/mnt/onboard/.metadata.json"

// As of plato 0.8.5: completion status of each book now stored as a separate file in .reading-states
const readingStatesPath = "/mnt/onboard/.reading-states/"

type fingerprint uint64

// Used as key in .metadata.json
func (f fingerprint) String() string {
	return strconv.FormatUint(uint64(f), 10)
}

// Used as filename in .reading-state/
func (f fingerprint) Hex() string {
	return fmt.Sprintf("%016X", uint64(f))
}

type platoMetadata struct {
	title      interface{}
	author     interface{}
	year       interface{}
	publisher  interface{}
	categories interface{}
	File       platoFileMetadata
	Reader     platoMetadataReader `json:"reader,omitempty"`
	added      interface{}
}

type platoFileMetadata struct {
	Path string
	kind interface{}
	size interface{}
}

type platoMetadataReader struct {
	opened      interface{}
	CurrentPage int `json:"currentPage"`
	pagesCount  interface{}
	Finished    bool `json:"finished"`
	rotation    interface{}
	fontSize    interface{}
}

func parsePlatoLegacyMetadata(path string) (meta []platoMetadata, err error) {
	raw, err := ioutil.ReadFile(path)
	if err != nil {
		return meta, err
	}
	err = json.Unmarshal(raw, &meta)
	return meta, err
}

func parsePlatoMetadata(metadataPath, readingStatesDirPath string) (meta map[string]platoMetadata, err error) {
	raw, err := ioutil.ReadFile(metadataPath)
	if err != nil {
		return meta, err
	}
	err = json.Unmarshal(raw, &meta)

	for id := range meta {
		u, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			log.Printf("failed to parse %s to uint64: %s\n", id, err)
			continue
		}
		fingerprint := fingerprint(u)
		readingStatePath := fmt.Sprintf("%s/%s.json", readingStatesDirPath, fingerprint.Hex())
		readingState, err := ioutil.ReadFile(readingStatePath)
		if err != nil {
			debugf("no reading state for : %s", readingStatePath)
			continue
		}

		var r platoMetadataReader
		err = json.Unmarshal(readingState, &r)
		if err != nil {
			log.Printf("failed to unmarshal %s: %s\n", readingStatePath, err)
			continue
		}

		entry := meta[id]
		entry.Reader = r
		meta[id] = entry
	}

	return meta, err
}

func checkPlatoStatus(bookPath string) (res bookStatus) {
	if len(legacyMeta) > 0 {
		return checkLegacyPlatoStatus(bookPath)
	}

	fingerprint, err := getFingerprint(bookPath)
	if err != nil {
		log.Printf("unable to get fingerprint for book path %s: %s", bookPath, err)
		return bookUnread
	}

	entry, ok := meta[fingerprint.String()]
	if !ok {
		log.Printf("no entry for fingerprint %s from book path %s in .metadata.json", fingerprint, bookPath)
		return bookUnread
	}

	if entry.Reader.Finished {
		debugf("book found as read: %s", bookPath)
		return bookRead
	} else if entry.Reader.CurrentPage != 0 {
		return bookReading
	}

	return bookUnread
}

func checkLegacyPlatoStatus(bookPath string) (res bookStatus) {
	for _, entry := range legacyMeta {
		if strings.HasSuffix(entry.File.Path, bookPath) {
			if entry.Reader.Finished {
				debugf("book found as read: %s", bookPath)
				return bookRead
			} else if entry.Reader.CurrentPage != 0 {
				return bookReading
			}
		} else if entry.Reader.Finished {
			debugf("book found as read but not matching pattern, expected: %s, actual: %s", bookPath, entry.File.Path)
		}
	}

	return bookUnread
}

var (
	parsed     bool
	legacyMeta []platoMetadata
	meta       map[string]platoMetadata

	// used for fingerprinting as of https://github.com/baskerville/plato/releases/tag/0.8.5
	fat32Epoch = time.Unix(315_532_800, 0)
)

func readPlatoStatus(ID int) (res bookStatus, err error) {
	if !parsed {
		meta, err = parsePlatoMetadata(metadataPath, readingStatesPath)
		if err != nil {
			legacyMeta, err = parsePlatoLegacyMetadata(metadataPath)
			if err != nil {
				return res, err
			}
		}

		parsed = true
		log.Println("loaded Plato config from ", metadataPath)
	}
	// XXX: similar code in readKoreaderStatus, getting messy and hardcode-y
	path := fmt.Sprintf("wallabako/%d.epub", ID)
	return checkPlatoStatus(path), err
}

func getFingerprint(path string) (f fingerprint, err error) {
	fileMeta, err := os.Stat(path)
	if err != nil {
		return f, err
	}

	mtime := fileMeta.ModTime()
	size := fileMeta.Size()

	diff := int64(mtime.Sub(fat32Epoch).Seconds())

	f = fingerprint(uint64((diff << 32) ^ size))

	return f, nil
}
