package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

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
	Verbose       bool `toml:"Verbose"`
	RetainedFiles int  `toml:"RetainedFiles"`
}

type outputConfig struct {
	Path   string `toml:"Path"`
	Delete bool   `toml:"Delete"`
}

// loadConfig opens and decodes the TOML config at path. Parse, unknown-key, and
// close failures are returned.
func loadConfig(path string) (_ appConfig, returnErr error) {
	f, err := os.Open(path)
	if err != nil {
		return appConfig{}, fmt.Errorf("open config file %s: %w", path, err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, f.Close())
	}()

	var cfg appConfig
	metadata, err := toml.NewDecoder(f).Decode(&cfg)
	if err != nil {
		return appConfig{}, fmt.Errorf("decode config file %s: %w", path, err)
	}
	if keys := metadata.Undecoded(); len(keys) > 0 {
		return appConfig{}, fmt.Errorf("unknown keys: %v", keys)
	}
	return cfg, nil
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
	if c.Log.RetainedFiles < 1 {
		return fmt.Errorf("Log.RetainedFiles must be greater than 0")
	}
	if c.Log.RetainedFiles > 100 {
		return fmt.Errorf("Log.RetainedFiles must not exceed 100")
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
