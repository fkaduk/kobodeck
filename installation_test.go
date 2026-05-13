package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// buildLinuxBinary compiles kobodeck for linux/arm (ARMv7) matching the Kobo target.
func buildLinuxBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "kobodeck")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=arm", "GOARM=7")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}
	return bin
}

// startKoboContainer starts a container simulating a Kobo device:
//   - kobodeck and companion files installed under /usr/local
//   - /mnt/onboard mounted as a FAT32 loop device (matching Kobo's vfat partition,
//     including 2-second mtime precision and case-insensitive filenames)
func startKoboContainer(t *testing.T, ctx context.Context, binaryPath string) testcontainers.Container {
	t.Helper()
	const setup = "apk add -q dosfstools && " +
		"mkdir -p /mnt/onboard && " +
		"dd if=/dev/zero of=/fat32.img bs=1M count=16 2>/dev/null && " +
		"mkfs.vfat /fat32.img && " +
		"mount -o loop /fat32.img /mnt/onboard && " +
		"exec sleep 60"
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:         "arm32v7/alpine:latest",
			ImagePlatform: "linux/arm/v7",
			Privileged:    true,
			Cmd:           []string{"sh", "-c", setup},
			Files: []testcontainers.ContainerFile{
				{
					HostFilePath:      binaryPath,
					ContainerFilePath: "/usr/local/bin/kobodeck",
					FileMode:          0755,
				},
				{
					HostFilePath:      "root/etc/udev/rules.d/90-kobodeck.rules",
					ContainerFilePath: "/etc/udev/rules.d/90-kobodeck.rules",
					FileMode:          0644,
				},
			},
			WaitingFor: wait.ForExec([]string{"sh", "-c", "grep -q /mnt/onboard /proc/mounts"}).
				WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start kobo container: %v", err)
	}
	t.Cleanup(func() { ctr.Terminate(ctx) })
	return ctr
}

// koboExec runs a command in the container and returns stdout+stderr.
// Fails the test if the command exits non-zero.
func koboExec(t *testing.T, ctx context.Context, ctr testcontainers.Container, cmd []string) string {
	t.Helper()
	code, out := koboRun(t, ctx, ctr, cmd)
	if code != 0 {
		t.Fatalf("exec %v exited %d:\n%s", cmd, code, out)
	}
	return out
}

// koboRun runs a command and returns its exit code and combined output.
func koboRun(t *testing.T, ctx context.Context, ctr testcontainers.Container, cmd []string) (int, string) {
	t.Helper()
	code, reader, err := ctr.Exec(ctx, cmd)
	if err != nil {
		t.Fatalf("exec %v: %v", cmd, err)
	}
	var stdout, stderr bytes.Buffer
	stdcopy.StdCopy(&stdout, &stderr, reader)
	return code, stdout.String() + stderr.String()
}

func TestKoboNoConfigCreatesTemplate(t *testing.T) {
	ctx := context.Background()
	ctr := startKoboContainer(t, ctx, buildLinuxBinary(t))

	// Run kobodeck with no config present — should write template and exit cleanly.
	if code, _ := koboRun(t, ctx, ctr, []string{"/usr/local/bin/kobodeck"}); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	// Template must exist and be non-empty.
	koboExec(t, ctx, ctr, []string{"test", "-s", "/mnt/onboard/.adds/kobodeck/kobodeck.toml"})

	// Log must mention the template.
	logContent := koboExec(t, ctx, ctr, []string{"cat", "/mnt/onboard/.adds/kobodeck/kobodeck.log"})
	if !strings.Contains(logContent, "template written") {
		t.Errorf("expected 'template written' in log, got:\n%s", logContent)
	}
}

func TestKoboEmptyConfigUninstalls(t *testing.T) {
	ctx := context.Background()
	ctr := startKoboContainer(t, ctx, buildLinuxBinary(t))

	// Place an empty config — this is how the user requests uninstall.
	koboExec(t, ctx, ctr, []string{"sh", "-c",
		"mkdir -p /mnt/onboard/.adds/kobodeck && " +
			"touch /mnt/onboard/.adds/kobodeck/kobodeck.toml",
	})

	// Simulate a log file left over from a previous run.
	koboExec(t, ctx, ctr, []string{"sh", "-c",
		"echo 'previous run' > /mnt/onboard/.adds/kobodeck/kobodeck.log",
	})

	if code, _ := koboRun(t, ctx, ctr, []string{"/usr/local/bin/kobodeck"}); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	// All installed files and the config directory must be gone.
	for _, path := range []string{
		"/usr/local/bin/kobodeck",
		"/etc/udev/rules.d/90-kobodeck.rules",
		"/mnt/onboard/.adds/kobodeck",
	} {
		if exitCode, _ := koboRun(t, ctx, ctr, []string{"test", "-e", path}); exitCode == 0 {
			t.Errorf("expected %s to be deleted after uninstall", path)
		}
	}
}
