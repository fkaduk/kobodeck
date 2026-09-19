package main

import (
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed kobodeck.toml
var configTemplate []byte

type appConfig struct {
	Server serverConfig `toml:"Server"`
	Fetch  fetchConfig  `toml:"Fetch"`
	Sync   syncConfig   `toml:"Sync"`
	Log    logConfig    `toml:"Log"`
	Output outputConfig `toml:"Output"`
}

type serverConfig struct {
	URL     string `toml:"URL"`
	Token   string `toml:"Token"`
	Timeout int    `toml:"Timeout"`
}

type fetchConfig struct {
	Workers int    `toml:"Workers"`
	Limit   int    `toml:"Limit"`
	Labels  string `toml:"Labels"`
	Status  string `toml:"Status"`
}

type syncConfig struct {
	Archive             bool   `toml:"Archive"`
	FavouriteCollection string `toml:"FavouriteCollection"`
}

type logConfig struct {
	Verbose bool `toml:"Verbose"`
	Size    int  `toml:"Size"` // in MB
}

type outputConfig struct {
	Path   string `toml:"Path"`
	Delete bool   `toml:"Delete"`
}

// validate checks that all required config fields are present and sane.
func (c *appConfig) validate() error {
	if c.Server.URL == "" {
		return fmt.Errorf("Server.URL is required")
	}
	serverURL, err := url.Parse(c.Server.URL)
	if err != nil || (serverURL.Scheme != "http" && serverURL.Scheme != "https") || serverURL.Host == "" || serverURL.RawQuery != "" || serverURL.Fragment != "" {
		return fmt.Errorf("Server.URL must be a valid http or https URL with a host and no query or fragment")
	}
	if c.Server.Token == "" {
		return fmt.Errorf("Server.Token is required")
	}
	if c.Server.Timeout <= 0 {
		return fmt.Errorf("Server.Timeout must be greater than 0")
	}
	if c.Fetch.Workers <= 0 {
		return fmt.Errorf("Fetch.Workers must be greater than 0")
	}
	if c.Fetch.Workers > 32 {
		return fmt.Errorf("Fetch.Workers must not exceed 32")
	}
	if c.Fetch.Limit < 0 {
		return fmt.Errorf("Fetch.Limit must not be negative")
	}
	for _, status := range strings.Split(c.Fetch.Status, ",") {
		status = strings.TrimSpace(status)
		if status != "" && status != "unread" && status != "reading" && status != "read" {
			return fmt.Errorf("Fetch.Status contains invalid value %q", status)
		}
	}
	if c.Log.Size < 0 {
		return fmt.Errorf("Log.Size must not be negative")
	}
	if c.Output.Path == "" {
		return fmt.Errorf("Output.Path is required")
	}
	cleanOutputPath := filepath.Clean(c.Output.Path)
	if !filepath.IsAbs(c.Output.Path) || cleanOutputPath == filepath.VolumeName(cleanOutputPath)+string(filepath.Separator) {
		return fmt.Errorf("Output.Path must be an absolute path other than the filesystem root")
	}
	return nil
}

// TODO: shouldnt this stuff be at the top ?
const confPath = "/mnt/onboard/.adds/kobodeck/kobodeck.toml"

var errUninstallRequested = errors.New("uninstall requested")

// loadConfig decodes the TOML file at path.
// Returns os.ErrNotExist if the file is absent, errUninstallRequested if
// the file is empty, or an error for parse failures and unrecognised keys.
func loadConfig(path string) (_ appConfig, returnErr error) {
	f, err := os.Open(path)
	if err != nil {
		return appConfig{}, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, f.Close())
	}()
	info, err := f.Stat()
	if err != nil {
		return appConfig{}, err
	}
	if info.Size() == 0 {
		return appConfig{}, errUninstallRequested
	}
	var cfg appConfig
	md, err := toml.NewDecoder(f).Decode(&cfg)
	if err != nil {
		return appConfig{}, err
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return appConfig{}, fmt.Errorf("unknown keys: %v", keys)
	}
	return cfg, nil
}

// findConfig resolves the config path (--config flag or default) and loads it.
// For the default path only: if no config exists, a template is written there
// and the function returns errConfigCreated. If the config is empty,
// errUninstallRequested is returned.
func findConfig() (string, appConfig, error) {
	// TODO: explain this, i dont get it
	if *configFileFlag != "" {
		cfg, err := loadConfig(*configFileFlag)
		if err != nil {
			return "", appConfig{}, fmt.Errorf("load config %s: %w", *configFileFlag, err)
		}
		return *configFileFlag, cfg, nil
	}
	if _, err := os.Stat(confPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(confPath, configTemplate, 0o600); err != nil {
			return "", appConfig{}, fmt.Errorf("write config template: %w", err)
		}
		return confPath, appConfig{}, errConfigCreated
	}
	cfg, err := loadConfig(confPath)
	if err != nil {
		return "", appConfig{}, fmt.Errorf("load config %s: %w", confPath, err)
	}
	return confPath, cfg, nil
}

// TODO: also at the top
var errConfigCreated = errors.New("config template created")
