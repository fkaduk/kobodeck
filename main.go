package main

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
	"time"
)

// username required here, this is foolish:
// https://github.com/wallabag/wallabag/issues/2800
var configJSON = flag.String("config", "config.json", "file name of config JSON file")
var outputDir = flag.String("output", ".", "output directory to save files into")

func login(url string, client http.Client) {
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
	//wallabago.Config = config
	//log.Printf("unread", wallabago.GetEntries(0, -1, "", "", -1, -1, "").Total)
	//log.Printf("total", wallabago.GetNumberOfTotalArticles())
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(config.WallabagURL + "/login")
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
	form.Set("_username", config.UserName)
	form.Set("_password", config.UserPassword)
	form.Set("_csrf_token", string(matches[1]))
	form.Set("_remember_me", "on")
	form.Set("send", "")
	resp, err = client.PostForm(config.WallabagURL+"/login_check", form)
	log.Print(resp, err)

	// proper way will be through the API, but for now we hardcode this URL
	// https://github.com/wallabag/wallabag/pull/2372
	// only in 2.2: /api/entries/123/export.epub
	epubUrl := "https://lib3.net/wallabag/export/22895.epub"
	epub := path.Base(epubUrl)
	output := path.Join(*outputDir, epub)
	out, err := os.Create(output)
	defer out.Close()
	//body = wallabago.GetBodyOfAPIURL(epubUrl)
	//out.Write(body)
	resp, err = client.Get(epubUrl)
	log.Print(resp, err)
	defer resp.Body.Close()
	n, err := io.Copy(out, resp.Body)
	log.Printf("wrote %d bytes in file %s", n, output)
	// this would be the proper way:
	// _, err = io.Copy(out, resp.Body)
	// but we don't get the response back from wallabago - instead it reads the whole body
}
