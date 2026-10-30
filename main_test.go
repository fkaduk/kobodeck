package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireLockRejectsSecondProcess(t *testing.T) {
	// Given
	lockFilePath := filepath.Join(t.TempDir(), "kobodeck.lock")
	first, err := acquireLock(lockFilePath)
	if err != nil {
		t.Fatalf("first acquireLock: %v", err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Errorf("close first lock: %v", err)
		}
	})
	// When
	second, err := acquireLock(lockFilePath)
	// Then
	if second != nil {
		if closeErr := second.Close(); closeErr != nil {
			t.Errorf("close second lock: %v", closeErr)
		}
		t.Fatal("second acquireLock unexpectedly succeeded")
	}
	if err == nil || err.Error() != "already running" {
		t.Fatalf("second acquireLock error = %v, want already running", err)
	}
}

func TestNickelRescanReportsEventFailure(t *testing.T) {
	// Given
	nickelStatusPath := filepath.Join(t.TempDir(), "nickel-status")
	if err := os.Mkdir(nickelStatusPath, 0o700); err != nil {
		t.Fatal(err)
	}
	// When
	err := nickelRescan(nickelStatusPath)
	// Then
	if err == nil || !strings.Contains(err.Error(), "add event: open "+nickelStatusPath) {
		t.Fatalf("nickelRescan() error = %v", err)
	}
}

func writeUninstallFile(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "kobodeck"), []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func nonEmptyUninstallFile(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "kobodeck", "child"), 0o700); err != nil {
		t.Fatal(err)
	}
}

// TestUninstallApplication covers the uninstall guard and file removal. A bad
// binary path must abort before touching anything; a non-removable file must
// surface as a partial failure rather than a silent success.
func TestUninstallApplication(t *testing.T) {
	// Given
	originalInstallFiles := installFiles
	t.Cleanup(func() { installFiles = originalInstallFiles })

	tests := []struct {
		name       string
		binaryPath string
		prepare    func(t *testing.T, dir string)
		wantErr    string
		wantKept   bool // first install file must still exist after
		wantGone   bool // first install file must be removed after
	}{
		{name: "aborts when binary is not under /usr/local", binaryPath: "/tmp/kobodeck", prepare: writeUninstallFile, wantErr: "unexpected command path", wantKept: true},
		{name: "succeeds when installed files are already gone", binaryPath: "/usr/local/bin/kobodeck", wantErr: ""},
		{name: "removes existing installed files", binaryPath: "/usr/local/bin/kobodeck", prepare: writeUninstallFile, wantErr: "", wantGone: true},
		{name: "reports partial failure when a file cannot be removed", binaryPath: "/usr/local/bin/kobodeck", prepare: nonEmptyUninstallFile, wantErr: "uninstall partially failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			dir := t.TempDir()
			installFiles = []string{filepath.Join(dir, "kobodeck"), filepath.Join(dir, "rules")}
			if tt.prepare != nil {
				tt.prepare(t, dir)
			}
			firstFile := filepath.Join(dir, "kobodeck")
			// When
			err := uninstallApplication(tt.binaryPath)
			// Then
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("uninstallApplication = %v, want success", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("uninstallApplication error = %v, want containing %q", err, tt.wantErr)
			}
			if tt.wantKept {
				if _, statErr := os.Stat(firstFile); statErr != nil {
					t.Fatalf("install file removed although uninstall should have aborted: %v", statErr)
				}
			}
			if tt.wantGone {
				if _, statErr := os.Stat(firstFile); !os.IsNotExist(statErr) {
					t.Fatalf("install file not removed after successful uninstall: stat = %v", statErr)
				}
			}
		})
	}
}
