package main

// cargo-culted from wallabag-stats
// XXX: should probably be moved into wallabago:
// https://github.com/Strubbl/wallabag-stats/issues/1

import (
	"encoding/json"
	"io/ioutil"
	"log"

	"github.com/anarcat/wallabago"
)

func getConfig() (wallabago.WallabagConfig, error) {
	log.Printf("getConfig: file is %s", *configJSON)
	var config wallabago.WallabagConfig
	raw, err := ioutil.ReadFile(*configJSON)
	if err != nil {
		return config, err
	}
	config, err = readJSON(raw)
	return config, err
}

func readJSON(raw []byte) (wallabago.WallabagConfig, error) {
	var config wallabago.WallabagConfig
	err := json.Unmarshal(raw, &config)
	return config, err
}
