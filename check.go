package main

import (
	"fmt"
	"io"
)

// runCheck prints the active configuration and lists bookmarks that would be
// synced, without downloading anything. Used by the --check flag.
func (a app) runCheck(w io.Writer) error {
	output := fmt.Sprintf(`Configuration:
  URL:     %s
  Output:  %s
  Workers: %d
  Limit:   %d
  Delete:  %v
`, a.cfg.Server.URL, a.cfg.Output.Path, a.cfg.Fetch.Workers, a.cfg.Fetch.Limit, a.cfg.Output.Delete)
	if a.cfg.Fetch.Labels != "" {
		output += fmt.Sprintf("  Labels:  %s\n\n", a.cfg.Fetch.Labels)
	} else {
		output += "  Labels:  (all)\n\n"
	}
	if _, err := io.WriteString(w, output+"Connecting to Readeck... "); err != nil {
		return fmt.Errorf("write check output: %w", err)
	}
	entries, err := a.readeck.listBookmarks(a.cfg.Fetch)
	if err != nil {
		return err
	}
	matchedEntries, skipped := filterBookmarksByLabel(entries, a.cfg.Fetch.Labels)
	for _, entry := range matchedEntries {
		output += fmt.Sprintf("  %s — %s\n", entry.ID, entry.Title)
	}
	output += fmt.Sprintf("\n%d bookmarks to sync", len(matchedEntries))
	if skipped > 0 {
		output += fmt.Sprintf(", %d skipped (label filter)", skipped)
	}
	if _, err := io.WriteString(w, "OK\n\n"+output+"\n"); err != nil {
		return fmt.Errorf("write check output: %w", err)
	}
	return nil
}
