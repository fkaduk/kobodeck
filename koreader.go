package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"regexp"
	"strconv"
)

var koreaderStat = regexp.MustCompile(`\["([^"]+)"\] = "?([0-9.]+|\w+)"?,?`)

const koreaderStatusComplete string = "complete"

func parseKoreaderStatus(metadataPath string) (res bookStatus, err error) {
	// for path.epub, we look in path.sdr/metadata.epub.lua for regex:
	//
	// ^\s*\["percent_finished"\] = [0-9.]+,?$
	//
	// ... and turn that number in a percentage. presumably if 100.0%
	// we are done, but maybe define a threshold?
	raw, err := ioutil.ReadFile(metadataPath)
	if err != nil {
		debugf("cannot read metadata file: %s", metadataPath)
		return res, err
	}
	matches := koreaderStat.FindAllStringSubmatch(string(raw), -1)
	percent := 0.0

	for _, match := range matches {
		key := match[1]
		value := match[2]
		switch {
		case key == "status":
			if value == koreaderStatusComplete {
				debugf("book %s completed", metadataPath)
				res = bookRead
			} else {
				debugf("koread book %s not complete: %q\n", metadataPath, value)
			}
		case key == "percent_finished":
			percent, err = strconv.ParseFloat(value, 32)
			if err != nil {
				log.Printf("failed to parse percent_finished %s in %s\n", value, metadataPath)
			} else {
				debugf("found koreader percent %f in %s", percent, metadataPath)
			}
		}
	}
	// process percentage status, while not clobbering a possible
	// "complete" status
	if res != bookRead {
		if percent >= 1 {
			res = bookRead
		} else if percent <= 0 {
			res = bookUnread
		} else {
			res = bookReading
		}
	}

	if len(matches) <= 0 {
		log.Printf("could not find koreader percent or status in %s, malformed metadata?\n", metadataPath)
	} else {
		debugf("koreader status of %s is %d\n", metadataPath, res)
	}

	return res, err
}

func readKoreaderStatus(ID int, outputDir string) (res bookStatus, err error) {
	metadataPath := fmt.Sprintf("%s/%d.sdr/metadata.epub.lua", outputDir, ID)
	res, err = parseKoreaderStatus(metadataPath)
	if err != nil {
		return res, err
	}
	return res, err
}
