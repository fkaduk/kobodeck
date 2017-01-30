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
	"sync"
	"time"

	"github.com/Strubbl/wallabago"
)

// username required here, this is foolish:
// https://github.com/wallabag/wallabag/issues/2800
var configJSON = flag.String("config", "config.json", "file name of config JSON file")
var outputDir = flag.String("output", ".", "output directory to save files into")
var count = flag.Int("count", 10, "number of articles to fetch")

// cargo-culted from:
// http://stackoverflow.com/questions/18207772/how-to-wait-for-all-goroutines-to-finish-without-using-time-sleep
// probably wrong.
var wg sync.WaitGroup

// this is necessary because < 2.2 don't have a EPUB API
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

// lightweight version of a wallabag entry with just what we need
type Entry struct {
	id      int
	changed time.Time
}

/*
the data format we receive from the API
{
  "page": 1,
  "limit": 30,
  "pages": 3,
  "total": 64,
  "_links": {
    "self": {
      "href": "https://example.net/wallabag/api/entries?archive=0&sort=created&order=desc&tags=&since=0&page=1&perPage=30"
    },
    "first": {
      "href": "https://example.net/wallabag/api/entries?archive=0&sort=created&order=desc&tags=&since=0&page=1&perPage=30"
    },
    "last": {
      "href": "https://example.net/wallabag/api/entries?archive=0&sort=created&order=desc&tags=&since=0&page=3&perPage=30"
    },
    "next": {
      "href": "https://example.net/wallabag/api/entries?archive=0&sort=created&order=desc&tags=&since=0&page=2&perPage=30"
    }
  },
  "_embedded": {
    "items": [
      {
        "is_archived": 0,
        "is_starred": 0,
        "user_name": "joe",
        "user_email": "joe@example.com",
        "user_id": 3,
        "tags": [],
        "id": 23152
        "created_at": "2014-06-13T12:18:34-0400",
        "updated_at": "2016-11-29T20:02:16-0500",
        "annotations": [],
        "mimetype": "text/html",
        "reading_time": 5,
        "domain_name": "arstechnica.com",
        "_links": {
          "self": {
            "href": "/api/entries/1579"
          }
        }
      }
    ]
  }
}

*/

// list entries and parse resulting JSON, sending them to the given channel
func listEntries(base_url string, entries chan Entry) {
	entriesURL := base_url + "/api/entries.json?archive=0&sort=updated&order=desc&perPage=" + strconv.Itoa(*count)
	log.Println("fetching entries from", entriesURL)
	body := wallabago.GetBodyOfAPIURL(entriesURL)
	log.Printf("done. parsing %d bytes of JSON", len(body))
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		log.Fatal("failed to parse JSON from API: ", err)
	}
	data := parsed["_embedded"].(map[string]interface{})["items"].([]interface{})
	//log.Print("parsed: ", data)
	for _, v := range data {
		d := v.(map[string]interface{})
		changed, err := time.Parse("2006-01-02T15:04:05-0700", d["updated_at"].(string))
		if err != nil {
			log.Printf("can't parse date %s: %s", d["updated_at"], err)
			changed = time.Now()
		}
		entry := Entry{id: int(d["id"].(float64)), changed: changed}
		log.Printf("found entry %d modified on %s", entry.id, entry.changed)
		entries <- entry
	}
	close(entries)
}

// download a given entry in the right place
func download(client *http.Client, base_url string, entry Entry) {
	// proper way will be through the API, but for now we hardcode this URL
	// https://github.com/wallabag/wallabag/pull/2372
	// only in 2.2: /api/entries/123/export.epub
	defer wg.Done()
	//log.Println("received entry", entry)
	epubUrl := base_url + "/export/" + strconv.Itoa(entry.id) + ".epub"
	epub := path.Base(epubUrl)
	output := path.Join(*outputDir, epub)
	info, err := os.Stat(output)
	if err == nil && info.ModTime().After(entry.changed) && info.Size() > 0 {
		log.Printf("URL %s older than local file %s, skipped", epubUrl, output)
		return
	} else if err != nil {
		//log.Println("missing:", err)
	} else {
		//log.Printf("out of date: err: %s, modtime: %s, changed: %s, before? : %s", err, info.ModTime(), entry.changed, info.ModTime().Before(entry.changed))
	}
	log.Printf("downloading %s in %s", epubUrl, output)
	out, err := os.Create(output)
	if err != nil {
		log.Fatal("failed to create output file: ", err)
	}
	defer out.Close()
	//body = wallabago.GetBodyOfAPIURL(epubUrl)
	//out.Write(body)
	resp, err := client.Get(epubUrl)
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
	config, err := getConfig()
	if err != nil {
		log.Fatal(err.Error())
	}
	wallabago.Config = config
	/* entries := wallabago.GetEntries(0, -1, "", "", -1, -1, "")
	log.Printf("found %d unread entries", entries.Total)
	log.Print(entries)*/
	/* for entry := range entries {
		log.Print(entry)
	} */
	log.Println("logging in to", config.WallabagURL)
	client := login(config.WallabagURL, config.UserName, config.UserPassword)
	entries := make(chan Entry)
	go listEntries(config.WallabagURL, entries)
	for entry := range entries {
		//log.Println("dispatching", entry)
		wg.Add(1)
		go download(client, config.WallabagURL, entry)
	}
	wg.Wait()
}
