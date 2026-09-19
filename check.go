package main

import (
	"fmt"
	"io"
	"strings"
)

// runCheck prints the active configuration and lists bookmarks that would be
// synced, without downloading anything. Used by the --check flag.
func (a app) runCheck(w io.Writer) error {
	if _, err := io.WriteString(w, formatCheckConfig(a.cfg)+"Connecting to Readeck... "); err != nil {
		return fmt.Errorf("write check output: %w", err)
	}
	entries, err := a.readeck.listBookmarks(a.cfg.Fetch)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "OK\n\n"+formatCheckEntries(a.cfg, entries)); err != nil {
		return fmt.Errorf("write check output: %w", err)
	}
	return nil
}

// TODO: why is a separate function needed here? runCheck already takes ioWriter ?
func writeCheckOutput(w io.Writer, cfg appConfig, entries []readeckBookmark) error {
	_, err := io.WriteString(w, formatCheckConfig(cfg)+"Connecting to Readeck... OK\n\n"+formatCheckEntries(cfg, entries))
	return err
}

func formatCheckConfig(cfg appConfig) string {
	output := fmt.Sprintf(`Configuration:
  URL:     %s
  Output:  %s
  Workers: %d
  Limit:   %d
  Delete:  %v
`, cfg.Server.URL, cfg.Output.Path, cfg.Fetch.Workers, cfg.Fetch.Limit, cfg.Output.Delete)
	if cfg.Fetch.Labels != "" {
		output += fmt.Sprintf("  Labels:  %s\n\n", cfg.Fetch.Labels)
	} else {
		output += "  Labels:  (all)\n\n"
	}
	return output
}

// TODO: this replicates core functionality. is there a better way?
// formatCheckEntries lists entries matching the configured label filter.
func formatCheckEntries(cfg appConfig, entries []readeckBookmark) string {
	labelFilter := make(map[string]bool)
	if cfg.Fetch.Labels != "" {
		for _, l := range strings.Split(strings.ToLower(cfg.Fetch.Labels), ",") {
			labelFilter[strings.TrimSpace(l)] = true
		}
	}

	var matched, skipped int
	var output string
	for _, entry := range entries {
		if len(labelFilter) > 0 && !matchesLabelFilter(labelFilter, entry.Labels) {
			skipped++
			continue
		}
		matched++
		output += fmt.Sprintf("  %s — %s\n", entry.ID, entry.Title)
	}
	output += fmt.Sprintf("\n%d bookmarks to sync", matched)
	if skipped > 0 {
		output += fmt.Sprintf(", %d skipped (label filter)", skipped)
	}
	return output + "\n"
}
