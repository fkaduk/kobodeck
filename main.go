package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/nightlyone/lockfile"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	configFileFlag = flag.String("config", "", "path to the configuration file")
	showVersion    = flag.Bool("version", false, "show program version and exit")
)

type readeckoboConfig struct {
	ReadeckURL     string `json:"ReadeckURL"`
	Token          string `json:"Token"`
	Debug          bool   `json:"debug"`
	Delete         bool   `json:"delete"`
	LogFile        string `json:"logfile"`
	Database       string `json:"Database"`
	Concurrency    int    `json:"Concurrency"`
	Count          int    `json:"Count"`
	Exec           string `json:"Exec"`
	OutputDir      string `json:"OutputDir"`
	PidFile        string `json:"PidFile"`
	Timeout        int    `json:"Timeout"`
	Tags           string `json:"Tags"`
	Uninstall      bool   `json:"Uninstall"`
	UninstallCerts bool   `json:"UninstallCerts"`
}

var config = readeckoboConfig{
	Database:    "/mnt/onboard/.kobo/KoboReader.sqlite",
	Concurrency: 2,
	Count:       -1,
	Timeout:     300,
}

type readeckBookmark struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	Updated    time.Time `json:"updated"`
	IsArchived bool      `json:"is_archived"`
	Labels     []string  `json:"labels"`
	Loaded     bool      `json:"loaded"`
}

func init() {
	flag.BoolVar(&config.Debug, "debug", false, "additional debugging information in logs")
	flag.BoolVar(&config.Delete, "delete", false, "delete EPUB files not found in feed")
	flag.StringVar(&config.Database, "database", config.Database, "path to Kobo Nickel database")
	flag.IntVar(&config.Concurrency, "concurrency", config.Concurrency, "number of parallel downloads")
	flag.StringVar(&config.LogFile, "logfile", config.LogFile, "write logs to file, rotated automatically")
	flag.IntVar(&config.Count, "count", config.Count, "number of articles to fetch (-1 = all)")
	flag.StringVar(&config.Exec, "exec", "", "command to run when files have changed")
	flag.StringVar(&config.OutputDir, "output", ".", "output directory for EPUB files")
	flag.StringVar(&config.PidFile, "pidfile", "", "pidfile to prevent concurrent runs")
	flag.IntVar(&config.Timeout, "timeout", config.Timeout, "HTTP request timeout in seconds")
	flag.StringVar(&config.Tags, "tags", "", "comma-separated list of labels to filter for")
	flag.StringVar(&config.ReadeckURL, "url", config.ReadeckURL, "Readeck server URL")
	flag.StringVar(&config.Token, "token", config.Token, "Readeck API token")
	flag.BoolVar(&config.Uninstall, "uninstall", false, "uninstall readeckobo")
	flag.BoolVar(&config.UninstallCerts, "uninstallcerts", false, "also uninstall ca-certificates.crt")
}

var (
	counter = Status{}
	home    = os.Getenv("HOME")
	version = "undefined"
)

func main() {
	flag.Parse()
	configFile, configErr := findConfig()
	flag.Parse()
	debugf("config: %#v", config)

	if *showVersion {
		fmt.Println(version)
		return
	}
	setupLogging(config)
	debug.SetPanicOnFault(true)

	if configErr != nil {
		log.Fatal(configErr.Error())
	}
	log.Println("loaded configuration from", configFile)

	start := time.Now()
	defer func() {
		log.Printf("version %s completed in %s, processed: %d, downloaded: %d, size: %s, deleted: %d, read: %d, unread: %d",
			version,
			time.Since(start).Truncate(time.Millisecond),
			counter.Processed.Value(),
			counter.Downloaded.Value(),
			humanize.IBytes(uint64(counter.Bytes.Value())),
			counter.Deleted.Value(),
			counter.Read.Value(),
			counter.Unread.Value())
	}()

	if config.Uninstall {
		uninstall()
	}

	lock, err := getLock(config.PidFile)
	if err != nil {
		log.Fatal("cannot lock PID file: ", err)
	}
	defer lock.Unlock()

	log.Println("connecting to", config.ReadeckURL)
	if config.Token == "" {
		log.Fatal("no Token configured; create one in the Readeck UI and add it to the config file")
	}

	client := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	sem := make(chan bool, config.Concurrency)
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	entries, err := listEntries()
	if err != nil {
		log.Fatal(err)
	}

	valid := make(map[string]bool)
	tags := make(map[string]bool)
	if len(config.Tags) > 0 {
		for _, tag := range strings.Split(strings.ToLower(config.Tags), ",") {
			tags[strings.TrimSpace(tag)] = true
		}
	}

OuterLoop:
	for _, entry := range entries {
		if len(tags) > 0 && !checkTags(tags, entry.Labels) {
			debugf("skipping %s (not in tags)", entry.ID)
			continue
		}
		debugln("dispatching", entry.ID)
		valid[entry.ID] = true
		select {
		case sem <- true:
			go func(e readeckBookmark) {
				defer func() { <-sem }()
				if err := download(client, e); err != nil {
					log.Println("error downloading entry", e.ID, err)
				}
			}(entry)
		case sig := <-sigc:
			log.Println("got signal:", sig, ", waiting for downloads to finish...")
			break OuterLoop
		}
	}
	for i := 0; i < cap(sem); i++ {
		sem <- true
	}

	inspectLocalFiles(config, valid)

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

func debugln(args ...interface{}) {
	if config.Debug {
		log.Println(args...)
	}
}

func debugf(format string, args ...interface{}) {
	if config.Debug {
		log.Printf(format, args...)
	}
}

func setupLogging(cfg readeckoboConfig, extraWriters ...io.Writer) {
	var writers []io.Writer
	if len(cfg.LogFile) > 0 {
		writers = append(writers, &lumberjack.Logger{
			Filename:   cfg.LogFile,
			MaxSize:    1,
			MaxBackups: 7,
			MaxAge:     7,
		})
	}
	writers = append(writers, os.Stdout)
	writers = append(writers, extraWriters...)
	log.SetOutput(io.MultiWriter(writers...))
}

const confPath = "readeckobo.js"

var confPaths = []string{
	home + "/.config/" + confPath,
	home + "/." + confPath,
	"/mnt/onboard/." + confPath,
	"/etc/" + confPath,
}

func loadConfig(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, &config)
}

func findConfig() (string, error) {
	if *configFileFlag != "" {
		if err := loadConfig(*configFileFlag); err == nil {
			return *configFileFlag, nil
		}
	}
	for _, path := range confPaths {
		if err := loadConfig(path); err == nil {
			return path, nil
		}
		debugf("can't load config path: %v", path)
	}
	return "", fmt.Errorf("no config file found")
}

func uninstall() {
	log.Println("uninstall requested, clearing myself out")
	if !strings.HasPrefix(os.Args[0], "/usr/local") {
		log.Fatal("unexpected command path, aborting uninstall:", os.Args[0])
	}
	files := []string{
		"/etc/readeckobo.js",
		"/etc/udev/rules.d/90-readeckobo.rules",
		"/usr/local/bin/fake-connect-usb",
		"/usr/local/bin/readeckobo-run",
		"/usr/local/bin/readeckobo-run-direct",
		"/usr/local/bin/readeckobo",
	}
	if config.UninstallCerts {
		files = append(files, "/etc/ssl/certs/ca-certificates.crt")
	}
	var lastErr error
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			log.Printf("failed: %s", err)
			lastErr = err
		} else {
			log.Printf("deleted %s", file)
		}
	}
	if lastErr != nil {
		log.Fatal("uninstall partially failed")
	}
	log.Fatal("uninstall complete")
}

const pidPath = "readeckobo.pid"

var pidPaths = []string{
	"/var/run/" + pidPath,
	"/run/" + pidPath,
	"/run/user/" + strconv.Itoa(os.Getuid()) + "/" + pidPath,
	home + "/." + pidPath,
}

func getLock(path string) (lock lockfile.Lockfile, err error) {
	if len(path) > 0 {
		if path, err = filepath.Abs(path); err != nil {
			return lock, err
		}
		lock, _ = lockfile.New(path)
		return lock, lock.TryLock()
	}
OuterLoop:
	for _, path := range pidPaths {
		debugln("trying lockfile path", path)
		lock, _ = lockfile.New(path)
		err = lock.TryLock()
		switch err.(type) {
		case *os.PathError:
			debugln(err)
			continue OuterLoop
		default:
			break OuterLoop
		}
	}
	return lock, err
}

func listEntries() ([]readeckBookmark, error) {
	var all []readeckBookmark
	page := 1
	const limit = 100
	for {
		url := fmt.Sprintf("%s/api/bookmarks?status=unread&limit=%d&page=%d",
			config.ReadeckURL, limit, page)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("build list request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+config.Token)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list bookmarks: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("list bookmarks: unexpected status %s", resp.Status)
		}

		var pageItems []readeckBookmark
		if err = json.NewDecoder(resp.Body).Decode(&pageItems); err != nil {
			return nil, fmt.Errorf("decode bookmarks: %w", err)
		}
		all = append(all, pageItems...)

		tp, err := strconv.Atoi(resp.Header.Get("Total-Pages"))
		if err != nil || page >= tp || (config.Count > 0 && len(all) >= config.Count) {
			break
		}
		page++
	}
	total := len(all)
	if config.Count > 0 && len(all) > config.Count {
		all = all[:config.Count]
	}
	log.Printf("found %d unread bookmarks, will process %d", total, len(all))
	counter.Unread.Store(uint32(total))
	return all, nil
}

func checkTags(tags map[string]bool, labels []string) bool {
	for _, label := range labels {
		if tags[strings.ToLower(label)] {
			return true
		}
	}
	return false
}

func download(client *http.Client, entry readeckBookmark) error {
	counter.Processed.Inc()
	if err := os.MkdirAll(config.OutputDir, os.ModePerm); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	epubURL := config.ReadeckURL + "/api/bookmarks/" + entry.ID + "/article.epub"
	output := filepath.Join(config.OutputDir, entry.ID+".epub")

	info, err := os.Stat(output)
	if err == nil && info.ModTime().After(entry.Updated) && info.Size() > 0 {
		debugf("skipping %s: local file newer than bookmark (%s > %s)", output, info.ModTime(), entry.Updated)
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", output, err)
	}

	log.Printf("downloading %s to %s", epubURL, output)
	req, err := http.NewRequest("GET", epubURL, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", epubURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", epubURL, resp.Status)
	}

	out, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}
	defer out.Close()

	n, err := io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(output)
		return fmt.Errorf("write %s: %w", output, err)
	}
	counter.Downloaded.Inc()
	counter.Bytes.Add(uint32(n))
	log.Printf("wrote %s (%s) timestamp %s", output, humanize.IBytes(uint64(n)), entry.Updated)
	return nil
}

type bookStatus int

const (
	bookUnread bookStatus = iota
	bookReading
	bookRead
)

func readStatus(ID string, outputDir string) (bookStatus, error) {
	if len(config.Database) > 0 {
		res, err := readNickelStatus(ID, outputDir)
		debugf("nickel book %s status: %d", ID, res)
		return res, err
	}
	return bookUnread, nil
}

func inspectLocalFiles(cfg readeckoboConfig, valid map[string]bool) {
	outputDir := strings.TrimSuffix(cfg.OutputDir, "/")
	files, _ := filepath.Glob(outputDir + "/*.epub")
	debugln("local files to inspect:", files)
	for _, file := range files {
		uid := strings.TrimSuffix(filepath.Base(file), ".epub")
		if uid == "" {
			log.Println("skipping file with empty name:", file)
			continue
		}
		status, err := readStatus(uid, outputDir)
		if err != nil {
			log.Println(err)
			continue
		}
		if status == bookRead {
			if err = markAsRead(uid); err != nil {
				log.Println("failed to mark as read:", err)
			} else {
				valid[uid] = false
				counter.Read.Inc()
			}
		}
		if cfg.Delete && !valid[uid] {
			if status == bookReading {
				log.Printf("not deleting book currently being read: %s", file)
			} else if err = os.Remove(file); err != nil {
				log.Printf("warning: failed to remove %s: %s", file, err)
			} else {
				log.Println("deleted", file)
				counter.Deleted.Inc()
			}
		}
	}
}

func markAsRead(id string) error {
	log.Printf("marking entry %s as archived", id)
	body, _ := json.Marshal(map[string]bool{"is_archived": true})
	_, err := doAPI("PATCH", config.ReadeckURL+"/api/bookmarks/"+id, bytes.NewBuffer(body))
	return err
}

func doAPI(method, apiURL string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, apiURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)
	req.Header.Set("Content-Type", "application/json")
	if config.Debug {
		dump, _ := httputil.DumpRequestOut(req, true)
		debugf("request: %q", dump)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if config.Debug {
		dump, _ := httputil.DumpResponse(resp, true)
		debugf("response: %q", dump)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, fmt.Errorf("API %s %s: %s", method, apiURL, resp.Status)
	}
	return data, nil
}

func listOpenFds() []string {
	fds, _ := filepath.Glob("/proc/self/fd/*")
	var result []string
	for _, fd := range fds {
		if link, err := os.Readlink(fd); err == nil {
			result = append(result, link)
		}
	}
	return result
}
