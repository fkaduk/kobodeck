package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

var (
	configFileFlag = flag.String("config", "", "path to the configuration file")
	checkFlag      = flag.Bool("check", false, "validate config and show what would be synced, then exit")
)

// TODO: EXPLAIN how is this variable overwritten in releases?
var buildVersion = "dev"

func main() {
	flag.Parse()
	// TODO: EXPLAIN what this does
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigc)

	err := run(os.Stdout, os.Args[0], sigc)
	if err != nil {
		log.Fatal(err)
	}
}
