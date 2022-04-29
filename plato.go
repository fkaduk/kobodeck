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

const metadataFilename = ".metadata.json"

// As of plato 0.8.5: completion status of each book now stored as a separate file in .reading-states
const readingStatesDirName = ".reading-states/"

// Plato v0.8.5+ uses the modified time from a file using the fat32-epoch
const fat32EpochFilename = ".fat32-epoch"

// fat32EpochSeconds is the number of seconds between the UNIX epoch (1/1/70) and the FAT32 epoch (1/1/80)
const fat32EpochSeconds = 315532800

type fingerprint uint64

// Used as key in .metadata.json
func (f fingerprint) String() string {
	return strconv.FormatUint(uint64(f), 10)
}

// Used as filename in .reading-state/
func (f fingerprint) Hex() string {
	return fmt.Sprintf("%016X", uint64(f))
}

// PlatoConfig hols the plato-specific configuration that we store in
// the wallabako configuration, this is part of the wallabakoConfig
// struct in main.go
type PlatoConfig struct {
	// LibraryPath corresponds to [[libraries.path]] in Settings.toml for Plato
	LibraryPath string
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
	debugf("parsing Plato metadata file %s and %s", metadataPath, readingStatesDirPath)
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
	debugf("found %d elements in Plato metadata", len(meta))
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
	fat32EpochModTime time.Time
)

func readPlatoStatus(ID int, outputDir string) (res bookStatus, err error) {
	libraryPath := config.PlatoConfig.LibraryPath

	if libraryPath == "" {
		// This was the default in wallabako 1.3.1 and earlier
		libraryPath = "/mnt/onboard"
	}

	if !parsed {
		metadataPath := fmt.Sprintf("%s/%s", libraryPath, metadataFilename)
		readingStatesPath := fmt.Sprintf("%s/%s", libraryPath, readingStatesDirName)
		meta, err = parsePlatoMetadata(metadataPath, readingStatesPath)
		if err != nil {
			legacyMeta, err = parsePlatoLegacyMetadata(metadataPath)
			if err != nil {
				log.Println("could not load Plato metadata", err)
				parsed = true
				return res, err
			}
		}

		fat32EpochPath := fmt.Sprintf("%s/%s", libraryPath, fat32EpochFilename)
		fat32EpochModTime = getFat32EpochModifiedTime(fat32EpochPath)
		parsed = true
		log.Println("loaded Plato config from ", metadataPath)
	}
	// XXX: similar code in readKoboStatus, getting messy and hardcode-y
	path := fmt.Sprintf("%s/%d.epub", outputDir, ID)
	return checkPlatoStatus(path), err
}

// Try to retrieve plato's .fat32-epoch modified time or create our own
func getFat32EpochModifiedTime(fat32EpochPath string) time.Time {
	fatMeta, err := os.Stat(fat32EpochPath)
	if err != nil {
		fat32EpochTime := time.Unix(fat32EpochSeconds, 0)

		f, err := ioutil.TempFile("", "wallabako-fat32-epoch-*")

		// This fallback may lead to wallbako not actioning on read items due to time variances creating incorrect fingerprints
		if f == nil || err != nil {
			debugf("using %#v for epoch due to error: %s\n", fat32EpochModTime.Unix(), err)
			return fat32EpochTime
		}

		err = os.Chtimes(f.Name(), fat32EpochTime, fat32EpochTime)

		fatMeta, err = os.Stat(f.Name())
		if err != nil {
			debugf("using %#v for epoch due to error: %s\n", fat32EpochModTime.Unix(), err)
			return fat32EpochTime
		}
	}

	debugf("%s mtime is %#v\n", fatMeta.Name(), fatMeta.ModTime().Unix())
	return fatMeta.ModTime()
}

func getFingerprint(path string) (f fingerprint, err error) {
	fileMeta, err := os.Stat(path)
	if err != nil {
		return f, err
	}

	mtime := fileMeta.ModTime()
	size := fileMeta.Size()

	diff := int64(mtime.Sub(fat32EpochModTime).Seconds())

	f = fingerprint(uint64((diff << 32) ^ size))

	debugf("path %s: fingerprint %s, mtime %#v, mtime tz %s, size %#v, diff %#v\n",
		path, f, mtime.Unix(), mtime.Location().String(), size, diff)
	return f, nil
}
