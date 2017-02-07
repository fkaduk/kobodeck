package main

/*

This program is designed to download Wallabag entries on to the
local disk, and particularly Kobo ebook readers.

More details in the README.md file that comes with this program.

This is my first go program. Forgive me, because I have probably sinned.

*/

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Strubbl/wallabago"
	"github.com/nightlyone/lockfile"
)

// XXX: we shouldn't need to write the password down in the config:
// https://github.com/wallabag/wallabag/issues/2800
var (
	configJSON = flag.String("config", "", "file name of config JSON file")
	outputDir  = flag.String("output", ".", "output directory to save files into")
	count      = flag.Int("count", -1, "number of articles to fetch")
	doDelete   = flag.Bool("delete", false, "if we should delete EPUB files not found in feed")
	pidFile    = flag.String("pidfile", "", "pidfile to write to avoid multiple runs")

	// default is from web browsers, which are around 6-10: http://www.browserscope.org/?category=network
	concurrency = flag.Int("concurrency", 6, "number of downloads to process in parallel")

	notify = flag.String("exec", "", "execute the given command when files have changed")

	retryMax = flag.Int("retry", 4, "number of attempts to login the website, with exponential backoff delay")

	// this is a generic counter to safely count things across threads
	// we use it to count how many files we actually downloaded
	counter = SafeCounter{v: make(map[string]int)}

	// the regex for the CSRF token in the login page
	csrfRegexp = regexp.MustCompile(`"_csrf_token" +value="([^"]*)"`)

	// the home directory
	home = os.Getenv("HOME")
)

// confPath is the name of the default configuration file
const confPath = "wallabako.js"

// the actual list of config paths to check
var confPaths = []string{
	home + "/.config/" + confPath,
	home + "/." + confPath,
	// special: for Kobo readers, this is the user-visible directory,
	// allow users to store the config file there
	"/mnt/onboard/." + confPath,
	"/etc/" + confPath,
}

// findConfig looks for and loads the configuration file. it is either
// provided as `path` or, if that is empty, is searched for in a set
// of standard directories
func findConfig(path string) (err error) {
	if path != "" {
		return wallabago.ReadConfig(path)
	}
	for _, path := range confPaths {
		if err = wallabago.ReadConfig(path); err == nil {
			break
		}
	}
	return err
}

// XXX: this is necessary because < 2.2 don't have a EPUB API
func login(baseURL, username, password string) (*http.Client, error) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(baseURL + "/login")
	if err != nil {
		return client, fmt.Errorf("failed to get login page: %v", err)
	}
	defer resp.Body.Close()
	// error ignored: if this fails, the CSRF token will be missing
	// and the error will be catched below
	body, _ := ioutil.ReadAll(resp.Body)
	matches := csrfRegexp.FindSubmatch(body)
	if len(matches) > 0 {
		log.Println("CSRF token found:", resp.Status)
	} else {
		return client, fmt.Errorf("no CSRF token found? is this a wallabag instance?")
	}
	form := url.Values{}
	form.Set("_username", username)
	form.Set("_password", password)
	form.Set("_csrf_token", string(matches[1]))
	form.Set("_remember_me", "on")
	form.Set("send", "")
	resp, err = client.PostForm(baseURL+"/login_check", form)
	if err == nil && resp.StatusCode == 302 {
		loc, e := resp.Location()
		if e != nil || strings.HasSuffix(loc.String(), "/login") {
			return client, fmt.Errorf("login failed: wrong password?")
		}
	} else {
		// we *always* get a 302, this shouldn't happen
		return client, fmt.Errorf("login failed: %s (%v)", resp.Status, err)
	}
	log.Println("logged in successful:", resp.Status)
	return client, nil
}

// get the unread entries, most recent first, limited to the given count
func listEntries() []wallabago.Item {
	e := wallabago.GetEntries(0, -1, "updated", "desc", -1, *count, "")
	log.Printf("found %d unread entries", e.Total)
	return e.Embedded.Items
}

// download a given entry in the right place
func download(client *http.Client, baseURL string, entry wallabago.Item) (err error) {
	// XXX: proper way will be through the API, but for now we hardcode this URL
	// https://github.com/wallabag/wallabag/pull/2372
	// only in 2.2: /api/entries/123/export.epub
	counter.Inc("processed")
	//log.Println("received entry", entry)
	err = os.MkdirAll(*outputDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}
	epubURL := baseURL + "/export/" + strconv.Itoa(entry.ID) + ".epub"
	output := filepath.Join(*outputDir, path.Base(epubURL))
	info, err := os.Stat(output)
	if err == nil && info.ModTime().After(entry.UpdatedAt.Time) && info.Size() > 0 {
		log.Printf("URL %s older than local file %s, skipped", epubURL, output)
		return nil
	} else if err != nil {
		//log.Println("missing:", err)
	} else {
		//log.Printf("out of date: err: %s, modtime: %s, changed: %s, before? : %s", err, info.ModTime(), entry.changed, info.ModTime().Before(entry.changed))
	}
	log.Printf("downloading %s in %s", epubURL, output)
	out, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer out.Close()
	// XXX: see above. doesn't work through API yet.
	//body = wallabago.GetBodyOfAPIURL(epubURL)
	//out.Write(body)
	resp, err := client.Get(epubURL)
	if err != nil {
		return fmt.Errorf("download of %s failed: %v", epubURL, err)
	}
	//log.Println("received response:", resp, err)
	defer resp.Body.Close()
	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("can't write file: %v", err)
	}
	counter.Inc("downloaded")
	log.Printf("wrote %d bytes in file %s", n, output)
	return nil
}

func deleteMissing(outputDir string, valid map[int]bool) (deleted []string) {
	files, _ := filepath.Glob(outputDir + "/*.epub")
	//log.Println("files:", files, outputDir+"/*.epub")
	for _, file := range files {
		id, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))
		if err != nil {
			log.Println("skipping irreglar file", file)
			continue
		}
		if valid[id] {
			//log.Println("keeping file with valid id:", file)
		} else {
			log.Print("removing old file:", file)
			if err = os.Remove(file); err != nil {
				log.Printf("warning: failed to remove file %s: %s", file, err)
			} else {
				deleted = append(deleted, file)
			}
		}
	}
	return deleted
}

// the base name of the pidfile
const pidPath = "wallabako.pid"

// the actual pathnames to check
var pidPaths = []string{
	"/var/run/" + pidPath,
	"/run/" + pidPath,
	"/run/user/" + strconv.Itoa(os.Getuid()) + "/" + pidPath,
	home + "/." + pidPath,
}

// getLock creates a lock file with the given path or, if empty, in an
// appropriate location in a series of predefined locations.
//
// WARNING: this does *not* defer the Unlock method, since it's out of
// scope - that should be done by the caller
func getLock(path string) (lock lockfile.Lockfile, err error) {
	if len(path) > 0 {
		if path, err = filepath.Abs(path); err != nil {
			return lock, err
		}
		// only error possible is if we don't have an absolute path,
		// already handled
		lock, _ = lockfile.New(path)
		err = lock.TryLock()
		return lock, err
	}
OuterLoop:
	for _, path := range pidPaths {
		//log.Println("trying lockfile path", path)
		lock, _ = lockfile.New(path)
		err = lock.TryLock()
		switch err.(type) {
		case *os.PathError:
			// permission denied, wrong path and so on
			//log.Println(err)
			continue OuterLoop
		default:
			break OuterLoop
		}
	}
	return lock, err
}

func main() {
	start := time.Now()
	log.SetOutput(os.Stdout)
	defer func() {
		log.Printf("completed in %.2fs\n", time.Since(start).Seconds())
	}()
	flag.Parse()
	if err := findConfig(*configJSON); err != nil {
		log.Fatal("cannot load configuration file: ", err.Error())
	}
	lock, err := getLock(*pidFile)
	if err != nil {
		log.Fatal("Cannot lock PID file: ", err)
	}
	defer lock.Unlock()

	log.Println("logging in to", wallabago.Config.WallabagURL)
	//log.Println("username, password:", wallabago.Config.UserName, wallabago.Config.UserPassword)
	// retryCount is the number of logins wallabako will attempt
	// first attempt is 1 second and first attempt double the delay at each attempt
	var client *http.Client
	for retryCount := 0; retryCount <= *retryMax; retryCount++ {
		client, err = login(wallabago.Config.WallabagURL, wallabago.Config.UserName, wallabago.Config.UserPassword)
		if err == nil {
			break
		} else {
			str := err.Error()
			switch {
			case strings.Contains(str, "login failed"), strings.Contains(str, "CSRF token"):
				log.Fatal(err)
			case strings.Contains(str, "login page"):
				// "exponential backoff time", but not random
				// this will sleep:
				// 1s (total 1s)
				// 2s (3s)
				// 5s (8s)
				// 10s (18s)
				// 17s (35s)
				// so 35 seconds max.
				// linear would be:
				// 1
				// 3 4
				// 5 9
				// 7 16
				// 9 25
				// but second retry is one second later, we want that one faster.
				delay := time.Duration((1 + retryCount ^ 2)) * time.Second
				log.Printf("%s, sleeping %s", err, delay)
				time.Sleep(delay)
			}
		}
	}
	if err != nil {
		log.Fatal(err)
	}
	// this is a semaphore buffer that will limit the number of
	// threads running. taken from
	// http://jmoiron.net/blog/limiting-concurrency-in-go/ an
	// alternative is to use sync/errgroup:
	// https://play.golang.org/p/hNaeTjLwdv we don't need toplevel
	// error handling yet, so we stick with the semaphore channel
	// pattern
	sem := make(chan bool, *concurrency)
	entries := listEntries()
	valid := make(map[int]bool)
	for _, entry := range entries {
		//log.Println("dispatching", entry.ID)
		valid[entry.ID] = true
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
	var deleted []string
	if *doDelete {
		deleted = deleteMissing(*outputDir, valid)
	}
	log.Printf("processed: %d, downloaded: %d, deleted: %d",
		counter.Value("processed"), counter.Value("downloaded"), len(deleted))
	if len(*notify) > 0 && (counter.Value("downloaded") > 0 || len(deleted) > 0) {
		log.Println("running command", *notify)
		out, err := exec.Command(*notify).CombinedOutput()
		if err != nil {
			log.Fatal(err)
		}
		if len(out) > 0 {
			log.Println(string(out))
		}
	}
}
