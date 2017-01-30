package main

/* This program is designed to download Wallabag entries on to the
 * local disk, and particularly Kobo ebook readers.
 *
 * More details in the README.md file that comes with this program.
 *
 * This is my first go program. Forgive me, because I have probably sinned.
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
	"time"

	"github.com/Strubbl/wallabago"
)

// XXX: we shouldn't need to write the password down in the config:
// https://github.com/wallabag/wallabag/issues/2800
var configJSON = flag.String("config", "config.json", "file name of config JSON file")
var outputDir = flag.String("output", ".", "output directory to save files into")
var count = flag.Int("count", 10, "number of articles to fetch")

// default is from web browsers, which are around 6-10: http://www.browserscope.org/?category=network
var concurrency = flag.Int("concurrency", 6, "number of downloads to process in parallel")

// this is a generic counter to safely count things across threads
// we use it to count how many files we actually downloaded
var counter = SafeCounter{v: make(map[string]int)}

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
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	re := regexp.MustCompile(`"_csrf_token" +value="([^"]*)"`)
	matches := re.FindSubmatch(body)
	if len(matches) > 0 {
		log.Println("CSRF token found: ", resp.Status)
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
	if err != nil {
		log.Fatal("login failed:", err)
	}
	log.Println("logged in successful:", resp.Status)
	return client
}

// get the unread entries, most recent first, limited to the given count
func listEntries() []wallabago.Item {
	e := wallabago.GetEntries(0, -1, "updated", "desc", -1, *count, "")
	log.Printf("found %d unread entries", e.Total)
	return e.Embedded.Items
}

// download a given entry in the right place
func download(client *http.Client, baseURL string, entry wallabago.Item) {
	// XXX: proper way will be through the API, but for now we hardcode this URL
	// https://github.com/wallabag/wallabag/pull/2372
	// only in 2.2: /api/entries/123/export.epub
	counter.Inc("processed")
	//log.Println("received entry", entry)
	err := os.MkdirAll(*outputDir, os.ModePerm)
	if err != nil {
		log.Fatal("failed to create directory", *outputDir, err)
	}
	epubURL := baseURL + "/export/" + strconv.Itoa(entry.ID) + ".epub"
	output := path.Join(*outputDir, path.Base(epubURL))
	info, err := os.Stat(output)
	if err == nil && info.ModTime().After(entry.UpdatedAt.Time) && info.Size() > 0 {
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
	if err != nil {
		log.Println("download failed:", epubURL, err)
		return
	}
	//log.Println("received response:", resp, err)
	defer resp.Body.Close()
	n, err := io.Copy(out, resp.Body)
	if err != nil {
		log.Println("can't write file:", err)
		return
	}
	counter.Inc("downloaded")
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
	// this is a semaphore buffer that will limit the number of threads running
	// http://jmoiron.net/blog/limiting-concurrency-in-go/
	sem := make(chan bool, *concurrency)
	entries := listEntries()
	for _, entry := range entries {
		//log.Println("dispatching", entry.ID)
		// try to get a slot in the semaphore
		sem <- true
		// we got it, fork off a thread
		go func(e wallabago.Item) {
			// release the slot when finished
			defer func() { <-sem }()
			download(client, wallabago.Config.WallabagURL, e)
		}(entry)
	}
	// refill all the semaphore slots to make sure we wait for everyone
	for i := 0; i < cap(sem); i++ {
		sem <- true
	}
	log.Printf("processed: %d, downloaded: %d", counter.Value("processed"), counter.Value("downloaded"))
}
