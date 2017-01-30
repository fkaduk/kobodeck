package main

/*
 * This is my first go program. Forgive me, because I have probably sinned.
 *
 * Note that I partly blame the Wallabag API for not returning a clean
 *list of identifiers, and the wallabago API for not making it better.
 */

import (
	"encoding/json"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"time"

	"github.com/Strubbl/wallabago"
)

// username required here, this is foolish:
// https://github.com/wallabag/wallabag/issues/2800
var configJSON = flag.String("config", "config.json", "file name of config JSON file")
var outputDir = flag.String("output", ".", "output directory to save files into")

func login(baseUrl, username, password string) *http.Client {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(baseUrl + "/login")
	log.Print(resp, err)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	re := regexp.MustCompile(`"_csrf_token" +value="([^"]*)"`)
	matches := re.FindSubmatch(body)
	if len(matches) > 0 {
		log.Print("token found: ", string(matches[1]))
	} else {
		log.Fatal("no CSRF token found. boom: " + string(body))
	}
	form := url.Values{}
	form.Set("_username", username)
	form.Set("_password", password)
	form.Set("_csrf_token", string(matches[1]))
	form.Set("_remember_me", "on")
	form.Set("send", "")
	resp, err = client.PostForm(baseUrl+"/login_check", form)
	log.Print(resp, err)
	return client
}

func listIds(base_url string, ids chan int) {
	entriesURL := base_url + "/api/entries.json?archive=0"
	body := wallabago.GetBodyOfAPIURL(entriesURL)
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		log.Fatal("failed to parse JSON from API: ", err)
	}
	data := parsed["_embedded"].(map[string]interface{})["items"].([]interface{})
	//log.Print("parsed: ", data)
	for _, v := range data {
		d := v.(map[string]interface{})
		ids <- int(d["id"].(float64))
	}
}

func download(client *http.Client, base_url string, id int) {
	// proper way will be through the API, but for now we hardcode this URL
	// https://github.com/wallabag/wallabag/pull/2372
	// only in 2.2: /api/entries/123/export.epub
	epubUrl := base_url + "/export/" + strconv.Itoa(id) + ".epub"
	epub := path.Base(epubUrl)
	output := path.Join(*outputDir, epub)
	log.Printf("downloading %s in %s", epubUrl, output)
	out, err := os.Create(output)
	defer out.Close()
	//body = wallabago.GetBodyOfAPIURL(epubUrl)
	//out.Write(body)
	resp, err := client.Get(epubUrl)
	log.Print(resp, err)
	defer resp.Body.Close()
	n, err := io.Copy(out, resp.Body)
	log.Printf("wrote %d bytes in file %s", n, output)
	// this would be the proper way:
	// _, err = io.Copy(out, resp.Body)
	// but we don't get the response back from wallabago - instead it reads the whole body
}

func main() {
	start := time.Now()
	log.SetOutput(os.Stdout)
	defer func() {
		log.Printf("printElapsedTime: time elapsed %.2fs\n", time.Since(start).Seconds())
	}()
	flag.Parse()
	config, err := getConfig()
	if err != nil {
		log.Fatal(err.Error())
	}
	log.Println("main: setting wallabago.Config var")
	wallabago.Config = config
	/* entries := wallabago.GetEntries(0, -1, "", "", -1, -1, "")
	log.Printf("found %d unread entries", entries.Total)
	log.Print(entries)*/
	/* for entry := range entries {
		log.Print(entry)
	} */
	client := login(config.WallabagURL, config.UserName, config.UserPassword)
	ids := make(chan int)
	go listIds(config.WallabagURL, ids)
	for id := range ids {
		go download(client, config.WallabagURL, id)
	}
}
