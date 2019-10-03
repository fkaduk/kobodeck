package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
)

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
	CurrentPage int `json:"current_page"`
	pagesCount  interface{}
	Finished    bool `json:"finished"`
	rotation    interface{}
	fontSize    interface{}
}

func parsePlatoMetadata(path string) (meta []platoMetadata, err error) {
	raw, err := ioutil.ReadFile(path)
	if err != nil {
		return meta, err
	}
	err = json.Unmarshal(raw, &meta)
	return meta, err
}

func checkPlatoStatus(bookPath string) (res bookStatus) {
	for _, entry := range meta {
		if entry.Reader.Finished && strings.HasSuffix(entry.File.Path, bookPath) {
			debugf("book found as read: %s", bookPath)
			return bookRead
		}
		if entry.Reader.Finished {
			debugf("book found as read but not matching pattern, expected: %s, actual: %s", bookPath, entry.File.Path)
		} else if entry.Reader.CurrentPage != 0 {
			return bookReading
		}
	}
	return bookUnread
}

var (
	parsed bool
	meta   []platoMetadata
)

func readPlatoStatus(ID int) (res bookStatus, err error) {
	configPath := "/mnt/onboard/.metadata.json"
	if !parsed {
		meta, err = parsePlatoMetadata(configPath)
		if err != nil {
			return res, err
		}
		parsed = true
		log.Println("loaded Plato config from ", configPath)
	}
	// XXX: similar code in readKoreaderStatus, getting messy and hardcode-y
	path := fmt.Sprintf("wallabako/%d.epub", ID)
	return checkPlatoStatus(path), err
}
