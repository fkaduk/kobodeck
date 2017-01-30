package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Strubbl/wallabago"
)

// hardcoded for now, but we need to parse CLI args...
var configJSON = flag.String("config", "config.json", "file name of config JSON file")

func main() {
	start := time.Now()
	log.SetOutput(os.Stdout)
	defer func() {
		log.Printf("printElapsedTime: time elapsed %.2fs\n", time.Since(start).Seconds())
	}()
	flag.Parse()
	c, err := getConfig()
	if err == nil {
		log.Println("main: setting wallabago.Config var")
		wallabago.Config = c
	} else {
		log.Fatal(err.Error())
	}
	total := float64(wallabago.GetNumberOfTotalArticles())
	log.Printf("total", total)
	client := &http.Client{}
	resp, err := client.Get("https://lib3.net/wallabag/")
	log.Print(resp, err)
}
