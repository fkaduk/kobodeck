package main

/*

This program is designed to download Wallabag entries on to the
local disk, and particularly Kobo ebook readers.

More details in the README.md file that comes with this program.

This is my first go program. Forgive me, because I have probably sinned.

*/

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Strubbl/wallabago/v6"
	"github.com/dustin/go-humanize"
	"github.com/nightlyone/lockfile"
	"gopkg.in/natefinch/lumberjack.v2"
)

// commandline flags that are not in the config file
var (
	// XXX: we shouldn't need to write the password down in the config:
	// https://github.com/wallabag/wallabag/issues/2800
	configFile  = flag.String("config", "", "path to the configuration file")
	showVersion = flag.Bool("version", false, "show program version and exit")
)

// wallabakoConfig represents all configuration settings that can be
// read from the config file. others are only specified on the
// commandline
type wallabakoConfig struct {
	wallabago.WallabagConfig
	Debug       bool        `json:"debug"`
	Delete      bool        `json:"delete"`
	LogFile     string      `json:"logfile"`
	Database    string      `json:"Database"`
	Concurrency int         `json:"Concurrency"`
	Count       int         `json:"Count"`
	Exec        string      `json:"Exec"`
	OutputDir   string      `json:"OutputDir"`
	PidFile     string      `json:"PidFile"`
	RetryMax    int         `json:"RetryMax"`
	Tags        string      `json:"Tags"`
	PlatoConfig PlatoConfig `json:"plato"`
}

// config is the global configuration, as read from the config file
// and overridden by commandline flags
//
// the values we set here will be used by default by the UnmarshalJSON
// function, so they are in effect the default values for those flags
//
// only some of those flags are set because the zero values are good
// enough for the other flags
var config = wallabakoConfig{
	Database:    "/mnt/onboard/.kobo/KoboReader.sqlite",
	Concurrency: 6,
	Count:       -1,
	RetryMax:    4,
}

// init sets up the commandline flags. when you change this, also
// change the matching README section
func init() {
	flag.BoolVar(&config.Debug, "debug", false, "additional debugging information in logs, including confidential information")
	flag.BoolVar(&config.Delete, "delete", false, "if we should delete EPUB files not found in feed")
	flag.StringVar(&config.Database, "database", config.Database, "path to Kobo Nickel database")
	// default is from web browsers, which are around 6-10: http://www.browserscope.org/?category=network
	flag.IntVar(&config.Concurrency, "concurrency", config.Concurrency, "number of downloads to process in parallel")
	flag.IntVar(&config.Count, "count", config.Count, "number of articles to fetch")
	flag.StringVar(&config.Exec, "exec", "", "execute the given command when files have changed")
	flag.StringVar(&config.OutputDir, "output", ".", "output directory to save files into")
	flag.StringVar(&config.PidFile, "pidfile", "", "pidfile to write to avoid multiple runs")
	flag.IntVar(&config.RetryMax, "retry", config.RetryMax, "number of attempts to login the website, with exponential backoff delay")
	flag.StringVar(&config.Tags, "tags", "", "a comma-separated list of tags to filter for")
}

// various global variables
var (
	// this is a generic counter to safely count things across threads
	// we use it to count how many files we actually downloaded and
	// other statistics
	counter = Status{}

	// the regex for the CSRF token in the login page
	csrfRegexp = regexp.MustCompile(`"_csrf_token" +value="([^"]*)"`)

	// the home directory
	home = os.Getenv("HOME")

	// version is the program's version
	version = "undefined"
)

func main() {
	// this can't be initialized in the short form below otherwise it
	// shadows the global config
	var err error
	// load defaults from configuration file
	*configFile, err = findConfig()
	// need to bootstrap logfile first before we handle errors
	setupLogging(config)
	if err != nil {
		log.Fatal(err.Error())
	}
	log.Println("loaded configuration from", *configFile)
	flag.Parse()
	debugf("config after commandline parsing: %#v", config)
	if *showVersion {
		fmt.Println(version)
		return
	}
	start := time.Now()
	defer func() {
		log.Printf("version %s completed in %s\n", version, time.Since(start))
	}()
	lock, err := getLock(config.PidFile)
	if err != nil {
		log.Fatal("Cannot lock PID file: ", err)
	}
	defer lock.Unlock()

	if err = wallabago.ReadConfig(*configFile); err != nil {
		log.Fatal("cannot load configuration file: ", err.Error())
	}

	log.Println("logging in to", config.WallabagURL)
	debugf("username: %v, password: %v", config.UserName, config.UserPassword)
	// retryCount is the number of logins wallabako will attempt
	// first attempt is 1 second and first attempt double the delay at each attempt
	var client *http.Client
	for retryCount := 0; retryCount <= config.RetryMax; retryCount++ {
		client, err = login(config.WallabagURL, config.UserName, config.UserPassword)
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
				delay := time.Duration((1 + (retryCount * retryCount))) * time.Second
				log.Printf("%s, sleeping %s (%d/%d)", err, delay, retryCount, config.RetryMax)
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
	// other solutions in https://www.sohamkamani.com/golang/data-races/
	sem := make(chan bool, config.Concurrency)
	entries, err := listEntries()
	if err != nil {
		log.Fatal(err)
	}
	valid := make(map[int]bool)
	tags := make(map[string]bool)
	if len(config.Tags) > 0 {
		for _, tag := range strings.Split(strings.ToLower(config.Tags), ",") {
			tags[strings.TrimSpace(tag)] = true
		}
	}
	for _, entry := range entries {
		if len(config.Tags) > 0 {
			if checkTags(tags, entry.Tags) == false {
				debugf("skipping %d (not in Tags)", entry.ID)
				continue
			}
		}
		debugln("dispatching", entry.ID)
		valid[entry.ID] = true
		// try to get a slot in the semaphore
		sem <- true
		// we got it, fork off a thread
		go func(e wallabago.Item) {
			// release the slot when finished
			defer func() { <-sem }()
			if err = download(client, config.WallabagURL, e); err != nil {
				log.Println("error downloading entry", e.ID, err)
			}
		}(entry)
	}
	// refill all the semaphore slots to make sure we wait for everyone
	for i := 0; i < cap(sem); i++ {
		sem <- true
	}

	inspectLocalFiles(config, valid)
	log.Printf("processed: %d, downloaded: %d, size: %s, deleted: %d, read: %d",
		counter.Processed.Value(), counter.Downloaded.Value(), humanize.IBytes(uint64(counter.Bytes.Value())), counter.Deleted.Value(), counter.Read.Value())
	if config.Debug {
		fds := listOpenFds()
		log.Printf("%d open file descriptors: %s", len(fds), fds)
	}
	if len(config.Exec) > 0 && (counter.Downloaded.Value() > 0 || counter.Deleted.Value() > 0) {
		log.Println("running command", config.Exec)
		out, err := exec.Command(config.Exec).CombinedOutput()
		if err != nil {
			log.Fatal(err)
		}
		if len(out) > 0 {
			log.Println(string(out))
		}
	}
}

// debugln will log the given arguments using log.Println only if
// debugging (config.Debug) is enabled
func debugln(args ...interface{}) {
	if config.Debug {
		log.Println(args...)
	}
}

// debugf will log the given arguments using log.Printf only if
// debugging (config.Debug) is enabled
func debugf(fmt string, args ...interface{}) {
	if config.Debug {
		log.Printf(fmt, args...)
	}
}

// setupLogging configures logging to a rotate file using the
// lumberjack package, if it is configured in the config file
//
// XXX: we do not support the -logfile argument anymore, as we would
// need to reconfigure logging on the fly, which is clunky. users can
// just use shell redirection there anyways.
func setupLogging(config wallabakoConfig) {
	if len(config.LogFile) > 0 {
		fileLogger := &lumberjack.Logger{
			Filename:   config.LogFile,
			MaxSize:    1, //megabytes - ouch, too big! https://github.com/natefinch/lumberjack/issues/37
			MaxBackups: 7, //files
			MaxAge:     7, //days
		}
		log.SetOutput(io.MultiWriter(fileLogger, os.Stdout))
	} else {
		log.SetOutput(os.Stdout)
	}
}

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

// loadConfig parses the given configuration file and returns it
func loadConfig(configFile string) (err error) {
	raw, err := ioutil.ReadFile(configFile)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, &config)
}

// findConfig looks for and loads the configuration file. it is either
// provided as `path` or, if that is empty, is searched for in a set
// of standard directories
func findConfig() (path string, err error) {
	for _, path = range confPaths {
		if err = loadConfig(path); err == nil {
			break
		} else {
			debugf("can't load config path %v: %v", path, err)
		}
	}
	return path, err
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
		debugln("trying lockfile path", path)
		lock, _ = lockfile.New(path)
		err = lock.TryLock()
		switch err.(type) {
		case *os.PathError:
			// permission denied, wrong path and so on
			debugln(err)
			continue OuterLoop
		default:
			break OuterLoop
		}
	}
	return lock, err
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
	// and the error will be caught below
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
func listEntries() ([]wallabago.Item, error) {
	entries, err := wallabago.GetEntries(wallabago.APICall, 0, -1, "updated", "desc", -1, config.Count, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list entries in wallabag: %v", err)
	}
	log.Printf("found %d unread entries", entries.Total)
	return entries.Embedded.Items, err
}

// check item tags against tags set in config
func checkTags(tags map[string]bool, itemTags []wallabago.Tag) bool {
	for _, tag := range itemTags {
		if tags[strings.ToLower(tag.Label)] {
			return true
		}
	}
	return false
}

// download a given entry in the right place
func download(client *http.Client, baseURL string, entry wallabago.Item) (err error) {
	// XXX: proper way will be through the API, but for now we hardcode this URL
	// https://github.com/wallabag/wallabag/pull/2372
	// only in 2.2: /api/entries/123/export.epub
	counter.Processed.Inc()
	//debugln("received entry", entry)
	err = os.MkdirAll(config.OutputDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}
	epubURL := baseURL + "/export/" + strconv.Itoa(entry.ID) + ".epub"
	output := filepath.Join(config.OutputDir, path.Base(epubURL))
	info, err := os.Stat(output)
	if err == nil && info.ModTime().After(entry.UpdatedAt.Time) && info.Size() > 0 {
		debugf("URL %s older than local file %s, skipped (%s > %s)\n", epubURL, output, info.ModTime(), entry.UpdatedAt.Time)
		return nil
	} else if os.IsNotExist(err) {
		debugln("missing:", err)
	} else if err != nil {
		return fmt.Errorf("unexpected error checking existing file: %v", err)
	}
	if err != nil && info != nil {
		debugf("out of date: err: %s, modtime: %s", err, info.ModTime())
		debugf("changed: %s, before: %v", entry.UpdatedAt.Time, info.ModTime().Before(entry.UpdatedAt.Time))
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
	//debugln("received response:", resp, err)
	defer resp.Body.Close()
	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("can't write file: %v", err)
	}
	if n >= 0 {
		counter.Downloaded.Inc()
		counter.Bytes.Add(uint32(n))
		log.Printf("wrote %d bytes (%s) in file %s, timestamp %s", n, humanize.IBytes(uint64(n)), output, entry.UpdatedAt.Time)
	}
	return nil
}

// inspectLocalFiles looks into the given outputDir for files matching
// the N.epub pattern where N is a Wallabag content ID, and processes
// every entry to mark it as read on the wallabag site and delete it
// (if it's read)
func inspectLocalFiles(config wallabakoConfig, valid map[int]bool) {
	// strip the trailing slash to avoid matching problems. this
	// occurs, for example, when matching files against the Nickel
	// sqlite database
	outputDir := strings.TrimSuffix(config.OutputDir, "/")

	files, _ := filepath.Glob(outputDir + "/*.epub")
	debugln("local files to inspect:", files, outputDir+"/*.epub")
	for _, file := range files {
		id, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))
		if err != nil {
			log.Println("skipping irreglar file", file)
			continue
		}
		status, err := readStatus(id, outputDir)
		if err != nil {
			log.Println(err)
			continue
		}
		if status == bookRead {
			err = markAsRead(id)
			if err != nil {
				log.Println("failed to mark as read:", err)
			} else {
				// read books are now up for deletion on next check
				// anyways, speed that up so we can remove them now
				valid[id] = false
				counter.Read.Inc()
			}
		}
		if config.Delete && !valid[id] {
			if status == bookReading {
				log.Printf("not deleting book currently being read: %s", file)
			} else if err = os.Remove(file); err != nil {
				log.Printf("warning: failed to remove file %s: %s", file, err)
			} else {
				log.Println("deleted file", file)
				counter.Deleted.Inc()
			}
		}
	}
}

// the book statuses we know of, internal to wallabako. this currently
// is the same as koboBookStatus but might change in the future
type bookStatus int

const (
	bookUnread bookStatus = iota
	bookReading
	bookRead
)

// readStatus will return the read status of the given ID book, which
// should be either bookUnread, bookReading or bookRead
func readStatus(ID int, outputDir string) (res bookStatus, err error) {
	res, err = readPlatoStatus(ID, outputDir)
	if res != bookUnread {
		debugf("plato book %d status not unread: %d\n", ID, res)
		return res, err
	}
	res, err = readKoreaderStatus(ID, outputDir)
	if res != bookUnread {
		debugf("koreader book %d status not unread: %d\n", ID, res)
		return res, err
	}
	res, err = readNickelStatus(ID, outputDir)
	debugf("nickel book %d status: %d\n", ID, res)
	return res, err
}

// markAsRead marks the given wallabag article ID as read through the API
func markAsRead(id int) (err error) {
	log.Printf("marking entry %d as read", id)
	tmp := map[string]int{"archive": 1}
	body, _ := json.Marshal(tmp)
	_, err = doAPI("PATCH", config.WallabagURL+"/api/entries/"+strconv.Itoa(id)+".json", bytes.NewBuffer(body))
	return err
}

// doAPI sends an arbitrary API call to the Wallabag API, getting a
// new token in the process. it returns the body of the response in
// bytes and any possible errors returned by the API, particularly if
// the returned status code is not 200.
func doAPI(method string, url string, body io.Reader) (data []byte, err error) {
	// this is copied from getBodyOfAPIURL(), should probably be
	// factored out

	client := &http.Client{}
	token, err := wallabago.GetAuthTokenHeader()
	if err != nil {
		return data, err
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return data, err
	}
	req.Header.Add("Authorization", token)
	req.Header.Add("Content-Type", "application/json")
	debugln("method, url, body:", method, url, body)
	dump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		return data, err
	}
	debugf("sending request: %q", dump)
	resp, err := client.Do(req)
	if err != nil {
		return data, err
	}
	defer resp.Body.Close()
	dump, err = httputil.DumpResponse(resp, true)
	if err != nil {
		return data, err
	}
	//debugf("received response: %q", dump)
	data, err = ioutil.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return data, fmt.Errorf("error from the API: %s", resp.Status)
	}
	return data, err
}

// listOpenFds is a simple debug tool to show the currently opened files.
func listOpenFds() (fds []string) {
	fds, _ = filepath.Glob("/proc/self/fd/*")
	for _, fd := range fds {
		link, err := os.Readlink(fd)
		if err != nil {
			fds = append(fds, err.Error())
		} else {
			fds = append(fds, link)
		}
	}
	return fds
}
