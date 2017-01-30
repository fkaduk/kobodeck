package main

/*
 * This is my first go program. Forgive me, because I have probably sinned.
 *
 * Note that I partly blame the Wallabag API for not returning a clean
 * list of identifiers, and the wallabago API for not making it better.
 *
 * Next steps:
 * - cross-compile to ARMel (v5?)
 * - hook into wifi?
 * - sleep and reload?
 * - party?
 * - port to v2.2 API
 */

import (
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
	"sync"
	"time"

	"github.com/anarcat/wallabago"
)

// XXX: we shouldn't need to write the password down in the config:
// https://github.com/wallabag/wallabag/issues/2800
var configJSON = flag.String("config", "config.json", "file name of config JSON file")
var outputDir = flag.String("output", ".", "output directory to save files into")
var count = flag.Int("count", 10, "number of articles to fetch")

// cargo-culted from:
// http://stackoverflow.com/questions/18207772/how-to-wait-for-all-goroutines-to-finish-without-using-time-sleep
// XXX: probably unecessary? but without this, the download threads
// get killed when the channel is closed or, if we don't close it, it
// never finishes
var wg sync.WaitGroup

// XXX: this is necessary because < 2.2 don't have a EPUB API
func login(baseURL, username, password string) *http.Client {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(baseURL + "/login")
	log.Print(resp, err)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	re := regexp.MustCompile(`"_csrf_token" +value="([^"]*)"`)
	matches := re.FindSubmatch(body)
	if len(matches) > 0 {
		log.Print("CSRF token found")
	} else {
		log.Fatal("no CSRF token found? is this a wallabag instance?")
	}
	form := url.Values{}
	form.Set("_username", username)
	form.Set("_password", password)
	form.Set("_csrf_token", string(matches[1]))
	form.Set("_remember_me", "on")
	form.Set("send", "")
	resp, err = client.PostForm(baseURL+"/login_check", form)
	log.Print(resp, err)
	return client
}

// Entry is a lightweight version of a wallabag entry with just what we need
// XXX: premature optimization? i put that there because i had trouble
// parsing JSON in the first place... not sure it's still necessary
type Entry struct {
	id      int
	changed time.Time
}

// get the unread entries, most recent first, limited to the given count
func listEntries(entries chan Entry) {
	e := wallabago.GetEntries(0, -1, "updated", "desc", -1, *count, "")
	log.Printf("found %d unread entries", e.Total)
	for _, entry := range e.Embedded.Items {
		entries <- Entry{id: entry.ID, changed: entry.UpdatedAt.Time}
	}
	close(entries)
}

// download a given entry in the right place
func download(client *http.Client, baseURL string, entry Entry) {
	// XXX: proper way will be through the API, but for now we hardcode this URL
	// https://github.com/wallabag/wallabag/pull/2372
	// only in 2.2: /api/entries/123/export.epub
	defer wg.Done()
	//log.Println("received entry", entry)
	err := os.MkdirAll(*outputDir, os.ModePerm)
	if err != nil {
		log.Fatal("failed to create directory", *outputDir, err)
	}
	epubURL := baseURL + "/export/" + strconv.Itoa(entry.id) + ".epub"
	output := path.Join(*outputDir, path.Base(epubURL))
	info, err := os.Stat(output)
	if err == nil && info.ModTime().After(entry.changed) && info.Size() > 0 {
		log.Printf("URL %s older than local file %s, skipped", epubURL, output)
		return
	} else if err != nil {
		//log.Println("missing:", err)
	} else {
		//log.Printf("out of date: err: %s, modtime: %s, changed: %s, before? : %s", err, info.ModTime(), entry.changed, info.ModTime().Before(entry.changed))
	}
	log.Printf("downloading %s in %s", epubURL, output)
	out, err := os.Create(output)
	if err != nil {
		log.Fatal("failed to create output file: ", err)
	}
	defer out.Close()
	// XXX: see above. doesn't work through API yet.
	//body = wallabago.GetBodyOfAPIURL(epubURL)
	//out.Write(body)
	resp, err := client.Get(epubURL)
	log.Println("received response:", resp, err)
	defer resp.Body.Close()
	n, err := io.Copy(out, resp.Body)
	log.Printf("wrote %d bytes in file %s", n, output)
}

func main() {
	start := time.Now()
	log.SetOutput(os.Stdout)
	defer func() {
		log.Printf("completed in %.2fs\n", time.Since(start).Seconds())
	}()
	flag.Parse()
	err := wallabago.ReadConfig(*configJSON)
	if err != nil {
		log.Fatal(err.Error())
	}
	log.Println("logging in to", wallabago.Config.WallabagURL)
	client := login(wallabago.Config.WallabagURL, wallabago.Config.UserName, wallabago.Config.UserPassword)
	entries := make(chan Entry)
	go listEntries(entries)
	for entry := range entries {
		//log.Println("dispatching", entry)
		wg.Add(1)
		go download(client, wallabago.Config.WallabagURL, entry)
	}
	wg.Wait()
}
