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

// buildLinuxBinary compiles kobodeck for linux/amd64 so it runs in the container.
func buildLinuxBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "kobodeck")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}
	return bin
}

// startKoboContainer starts a container simulating a Kobo device:
// - kobodeck and companion files installed under /usr/local
// - /mnt/onboard mounted as tmpfs (simulating the Kobo user storage partition)
func startKoboContainer(t *testing.T, ctx context.Context, binaryPath string) testcontainers.Container {
	t.Helper()
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "60"},
			Tmpfs: map[string]string{"/mnt/onboard": "rw"},
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
			WaitingFor: wait.ForExec([]string{"true"}).WithStartupTimeout(30 * time.Second),
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
	code, out := koboRun(t, ctx, ctr, []string{"/usr/local/bin/kobodeck"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "template written") {
		t.Errorf("expected 'template written' in output, got:\n%s", out)
	}

	// Template must exist and be non-empty.
	koboExec(t, ctx, ctr, []string{"test", "-s", "/mnt/onboard/.adds/kobodeck/kobodeck.toml"})
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

	code, out := koboRun(t, ctx, ctr, []string{"/usr/local/bin/kobodeck"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "uninstall complete") {
		t.Errorf("expected 'uninstall complete' in output, got:\n%s", out)
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
