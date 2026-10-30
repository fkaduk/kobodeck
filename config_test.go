package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newValidAppConfig(outputPath string) appConfig {
	return appConfig{
		Server: serverConfig{URL: "https://readeck.example/api", Token: "token", Timeout: 5},
		Fetch:  fetchConfig{Workers: 2, Limit: 10, Status: "unread,reading"},
		Log:    logConfig{RetainedFiles: 10},
		Output: outputConfig{Path: outputPath},
	}
}

const loadConfigValidTOML = `
[Server]
URL = "https://readeck.example/api"
Token = "token"
Timeout = 5

[Fetch]
Workers = 2
Limit = 10
Status = "unread,reading"

[Log]
RetainedFiles = 10

[Output]
Path = "/abs/output"
`

// TestLoadConfigDecodesValidConfig covers the happy path of loadConfig, which
// is exercised on every startup but was previously untested.
func TestLoadConfigDecodesValidConfig(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "kobodeck.toml")
	if err := os.WriteFile(path, []byte(loadConfigValidTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	// When
	cfg, err := loadConfig(path)
	// Then
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Server.URL != "https://readeck.example/api" || cfg.Server.Token != "token" || cfg.Server.Timeout != 5 {
		t.Errorf("Server = %+v, want URL/Token/Timeout set", cfg.Server)
	}
	if cfg.Fetch.Workers != 2 || cfg.Fetch.Limit != 10 || cfg.Fetch.Status != "unread,reading" {
		t.Errorf("Fetch = %+v", cfg.Fetch)
	}
	if cfg.Log.RetainedFiles != 10 {
		t.Errorf("Log.RetainedFiles = %d, want 10", cfg.Log.RetainedFiles)
	}
	if cfg.Output.Path != "/abs/output" {
		t.Errorf("Output.Path = %q, want /abs/output", cfg.Output.Path)
	}
}

// TestLoadConfigRejectsMalformedAndUnknownKeys guards the config-loading
// failure modes (unreadable file, invalid TOML, keys not in the config
// schema) that would otherwise surface as confusing runtime behaviour.
func TestLoadConfigRejectsMalformedAndUnknownKeys(t *testing.T) {
	// Given
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "malformed toml", body: "Server = ", wantErr: "decode config file"},
		{name: "unknown key", body: "Bogus = 1\n" + loadConfigValidTOML, wantErr: "unknown keys"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "kobodeck.toml")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			// When
			_, err := loadConfig(path)
			// Then
			if err == nil {
				t.Fatalf("loadConfig: %v, want error containing %q", nil, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("loadConfig error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}

	// The missing-file case needs no file on disk.
	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "does-not-exist.toml")
		// When
		_, err := loadConfig(path)
		// Then
		if err == nil || !strings.Contains(err.Error(), "open config file") {
			t.Fatalf("loadConfig error = %v, want an open error", err)
		}
	})
}

func TestAppConfigValidation(t *testing.T) {
	// Given
	outputPath := t.TempDir()
	tests := []struct {
		name   string
		mutate func(*appConfig)
		valid  bool
	}{
		{name: "http URL", mutate: func(c *appConfig) { c.Server.URL = "http://localhost:8080/readeck/" }, valid: true},
		{name: "URL with query", mutate: func(c *appConfig) { c.Server.URL = "https://readeck.example?token=x" }},
		{name: "URL with fragment", mutate: func(c *appConfig) { c.Server.URL = "https://readeck.example/#api" }},
		{name: "URL without host", mutate: func(c *appConfig) { c.Server.URL = "https:///api" }},
		{name: "URL with unsupported scheme", mutate: func(c *appConfig) { c.Server.URL = "ftp://readeck.example" }},
		{name: "too many workers", mutate: func(c *appConfig) { c.Fetch.Workers = 33 }},
		{name: "negative limit", mutate: func(c *appConfig) { c.Fetch.Limit = -1 }},
		{name: "empty status", mutate: func(c *appConfig) { c.Fetch.Status = "" }, valid: true},
		{name: "valid statuses", mutate: func(c *appConfig) { c.Fetch.Status = "unread, reading,read" }, valid: true},
		{name: "invalid status", mutate: func(c *appConfig) { c.Fetch.Status = "unread,finished" }},
		{name: "zero retained logs", mutate: func(c *appConfig) { c.Log.RetainedFiles = 0 }},
		{name: "too many retained logs", mutate: func(c *appConfig) { c.Log.RetainedFiles = 101 }},
		{name: "relative output", mutate: func(c *appConfig) { c.Output.Path = "books" }},
		{name: "root output", mutate: func(c *appConfig) { c.Output.Path = "/" }},
		{name: "absolute output", mutate: func(c *appConfig) { c.Output.Path = outputPath }, valid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidAppConfig(outputPath)
			tt.mutate(&cfg)
			// When
			err := cfg.validate()
			// Then
			if tt.valid && err != nil {
				t.Fatalf("validate() returned unexpected error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("validate() accepted invalid configuration")
			}
		})
	}
}
