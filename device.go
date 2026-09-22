package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

const (
	defaultNickelDBPath     = "/mnt/onboard/.kobo/KoboReader.sqlite"
	defaultNickelStatusPath = "/tmp/nickel-hardware-status"
	defaultLockFilePath     = "/tmp/kobodeck.lock"
)

var installFiles = []string{
	"/etc/udev/rules.d/90-kobodeck.rules",
	"/usr/local/bin/kobodeck",
}

func uninstallApplication(binaryPath string) error {
	if !strings.HasPrefix(binaryPath, "/usr/local") {
		return fmt.Errorf("unexpected command path, aborting uninstall: %s", binaryPath)
	}
	var uninstallErr error
	for _, file := range installFiles {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("failed to remove %s: %s", file, err)
			uninstallErr = errors.Join(uninstallErr, err)
		} else {
			log.Printf("deleted %s", file)
		}
	}
	if uninstallErr != nil {
		return fmt.Errorf("uninstall partially failed: %w", uninstallErr)
	}
	log.Println("uninstall complete")
	return nil
}

// nickelRescan triggers a Nickel library rescan by simulating a USB plug/unplug
// via /tmp/nickel-hardware-status.
func nickelRescan(statusPath string) error {
	log.Println("triggering Nickel rescan")
	if err := appendNickelEvent(statusPath, "add"); err != nil {
		return err
	}
	time.Sleep(10 * time.Second)
	return appendNickelEvent(statusPath, "remove")
}

// TODO: this isnt descriptive, also not sure why 2 functions are needed. This only needs to appends to udev?
// appendNickelEvent writes the status-file command Nickel uses to emulate USB events.
func appendNickelEvent(path, event string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("%s event: open %s: %w", event, path, err)
	}
	if _, err := f.WriteString("usb plug " + event + "\n"); err != nil {
		return errors.Join(fmt.Errorf("%s event: write %s: %w", event, path, err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%s event: close %s: %w", event, path, err)
	}
	return nil
}
