package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
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
	currentPage interface{}
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

func checkPlatoStatus(bookPath string) (res bool) {
	for _, entry := range meta {
		if entry.Reader.Finished && entry.File.Path == bookPath {
			debugf("book found as read: %s", bookPath)
			return true
		}
		if entry.Reader.Finished {
			debugf("book found as read but not matching pattern, expected: %s, actual: %s", bookPath, entry.File.Path)
		}
	}
	return res
}

var (
	parsed bool
	meta   []platoMetadata
)

func readPlatoStatus(ID int) (res bool, err error) {
	if !parsed {
		meta, err = parsePlatoMetadata("/mnt/onboard/.metadata.json")
	}
	if err != nil {
		return res, err
	}
	// XXX: similar code in readKoreaderStatus, getting messy and hardcode-y
	path := fmt.Sprintf("wallabako/%d.epub", ID)
	res = checkPlatoStatus(path)
	return res, err
}
