// This file contains the host-side controller for the minimal ARM system-VM
// smoke test.
//
// Readeck still runs outside the guest in a host-side container owned directly
// by TestKoboVMReadeckAPISmoke. The test uses the Docker CLI because managing
// one disposable container does not justify a container SDK and its dependency
// tree. The simulated Kobo is built entirely from Alpine's official, pinned
// ARMv7 netboot kernel and initramfs. A tiny second initramfs archive supplies
// testvm/smoke-init and the per-test Readeck URL/token.
//
// There is intentionally no guest root disk, Docker-built ARM image, FAT
// volume, eudev, or SSH server in this smoke test. Those belong in later tests
// that actually need Kobo storage and udev behavior.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	alpineNetbootBaseURL = "https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/armv7/netboot-3.24.1"
	artifactCacheDir     = ".cache/kobodeck-testvm"
	qemuHostGatewayIP    = "10.0.2.2"
	vmSmokeTimeout       = 60 * time.Second

	smokeResponseBegin = "KOBODECK_API_RESPONSE_BEGIN"
	smokeResponseEnd   = "KOBODECK_API_RESPONSE_END"

	readeckImage   = "codeberg.org/readeck/readeck:0.22.3"
	testAdminUser  = "testadmin"
	testAdminPass  = "testpass123"
	testAdminEmail = "testadmin@test.invalid"
)

var bearerRegexp = regexp.MustCompile(`value="Authorization: Bearer ([^"]+)"`)

// These hashes pin the exact upstream boot artifacts. A changed or corrupted
// download fails before QEMU starts.
var alpineNetbootArtifacts = []struct {
	name   string
	sha256 string
}{
	{
		name:   "vmlinuz-lts",
		sha256: "f36e6732b8165eb43780b86a87a3eb2e17fbabc9984bab9796a9a72c4d56debc",
	},
	{
		name:   "initramfs-lts",
		sha256: "32807f8009a39c86bc70d601e9175293d416a7d204275f807e490cd92653cd68",
	},
}

// docker runs the Docker CLI while keeping stdout separate from progress output
// written to stderr. This matters on the first run: pulling an absent image
// must not be mistaken for part of the container ID or port mapping.
func docker(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(output)), nil
}

// startHostReadeckServer owns the complete external-service lifecycle. The
// health loop completes before user/token setup and before QEMU can start, so
// the guest can never race an unready Readeck process.
func startHostReadeckServer(t *testing.T) (apiURL, token string) {
	t.Helper()
	ctx := t.Context()
	containerID, err := docker(
		ctx,
		"run", "--detach", "--rm",
		"--publish", "127.0.0.1::8000",
		readeckImage,
	)
	if err != nil {
		t.Fatalf("start host-side Readeck: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := docker(cleanupCtx, "stop", "--time", "1", containerID); err != nil &&
			!strings.Contains(err.Error(), "No such container") {
			t.Errorf("stop host-side Readeck: %v", err)
		}
	})

	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err = docker(ctx, "exec", containerID, "readeck", "healthcheck"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			logs, _ := docker(ctx, "logs", containerID)
			t.Fatalf("Readeck did not become healthy: %v\n%s", err, logs)
		}
		time.Sleep(250 * time.Millisecond)
	}

	if _, err := docker(
		ctx, "exec", containerID,
		"readeck", "user",
		"-user", testAdminUser,
		"-password", testAdminPass,
		"-email", testAdminEmail,
	); err != nil {
		t.Fatalf("create Readeck admin user: %v", err)
	}

	mapping, err := docker(ctx, "port", containerID, "8000/tcp")
	if err != nil {
		t.Fatalf("get Readeck mapped port: %v", err)
	}
	_, port, found := strings.Cut(mapping, ":")
	if !found || port == "" {
		t.Fatalf("parse Readeck port mapping %q", mapping)
	}

	token, err = bootstrapToken("http://127.0.0.1:" + port)
	if err != nil {
		t.Fatalf("bootstrap Readeck API token: %v", err)
	}
	return fmt.Sprintf("http://%s:%s/api/bookmarks", qemuHostGatewayIP, port), token
}

// bootstrapToken uses Readeck's web session because current Readeck versions
// do not provide an equivalent token-creation CLI command.
func bootstrapToken(baseURL string) (string, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	resp, err := client.PostForm(baseURL+"/login", url.Values{
		"username": {testAdminUser},
		"password": {testAdminPass},
		"redirect": {""},
	})
	if err != nil {
		return "", fmt.Errorf("login request: %w", err)
	}
	resp.Body.Close()

	resp, err = client.Post(baseURL+"/profile/tokens", "application/x-www-form-urlencoded", nil)
	if err != nil {
		return "", fmt.Errorf("token creation request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token page: %w", err)
	}
	matches := bearerRegexp.FindSubmatch(body)
	if len(matches) < 2 {
		return "", fmt.Errorf("token not found in Readeck profile page")
	}
	return string(matches[1]), nil
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("required command %q was not found: %v", name, err)
	}
}

func fileHasSHA256(path, wantHash string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)) == wantHash
}

// ensureNetbootArtifacts downloads each pinned Alpine artifact at most once.
// The files are small enough to verify in memory. If a download is interrupted,
// its hash fails and the next run simply downloads it again.
func ensureNetbootArtifacts(t *testing.T) (kernelPath, initramfsPath string) {
	t.Helper()
	if err := os.MkdirAll(artifactCacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	for _, artifact := range alpineNetbootArtifacts {
		path := filepath.Join(artifactCacheDir, artifact.name)
		if !fileHasSHA256(path, artifact.sha256) {
			requestURL := alpineNetbootBaseURL + "/" + artifact.name
			resp, err := client.Get(requestURL)
			if err != nil {
				t.Fatalf("download %s: %v", requestURL, err)
			}
			data, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("download %s: got HTTP %s", requestURL, resp.Status)
			}
			if readErr != nil {
				t.Fatalf("download %s: %v", requestURL, readErr)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != artifact.sha256 {
				t.Fatalf("verify %s: got sha256 %s, want %s", requestURL, got, artifact.sha256)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	return filepath.Join(artifactCacheDir, "vmlinuz-lts"),
		filepath.Join(artifactCacheDir, "initramfs-lts")
}

// buildSmokeInitramfs appends a gzip-compressed "newc" cpio archive to Alpine's
// initramfs. Linux supports a sequence of compressed cpio members, so the
// official archive remains byte-for-byte unchanged and our overlay adds only:
//
//   - /smoke-init
//   - /etc/kobodeck-smoke/url
//   - /etc/kobodeck-smoke/token
func buildSmokeInitramfs(t *testing.T, basePath, apiURL, token string) string {
	t.Helper()
	workDir := t.TempDir()
	overlayDir := filepath.Join(workDir, "overlay")
	configDir := filepath.Join(overlayDir, "etc", "kobodeck-smoke")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	initScript, err := os.ReadFile("testvm/smoke-init")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayDir, "smoke-init"), initScript, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "url"), []byte(apiURL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "token"), []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	combinedPath := filepath.Join(workDir, "initramfs-smoke")
	base, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(combinedPath, base, 0o600); err != nil {
		t.Fatal(err)
	}
	combined, err := os.OpenFile(combinedPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	gzipWriter := gzip.NewWriter(combined)
	cpio := exec.Command("cpio", "--create", "--format=newc", "--quiet")
	cpio.Dir = overlayDir
	cpio.Stdin = strings.NewReader(strings.Join([]string{
		"smoke-init",
		"etc",
		"etc/kobodeck-smoke",
		"etc/kobodeck-smoke/url",
		"etc/kobodeck-smoke/token",
	}, "\n") + "\n")
	cpio.Stdout = gzipWriter
	var cpioStderr bytes.Buffer
	cpio.Stderr = &cpioStderr
	if err := cpio.Run(); err != nil {
		gzipWriter.Close()
		combined.Close()
		t.Fatalf("build initramfs overlay: %v\n%s", err, cpioStderr.String())
	}
	if err := gzipWriter.Close(); err != nil {
		combined.Close()
		t.Fatal(err)
	}
	if err := combined.Close(); err != nil {
		t.Fatal(err)
	}
	return combinedPath
}

// runKoboVM blocks until smoke-init prints the API response and powers off the
// guest. CommandContext handles the only necessary failure cleanup: killing a
// VM that does not complete within vmSmokeTimeout.
func runKoboVM(t *testing.T, kernelPath, initramfsPath string) string {
	t.Helper()
	args := []string{
		"-machine", "virt",
		"-cpu", "cortex-a15",
		"-smp", "1",
		"-m", "256",
		"-nographic",
		"-no-reboot",
		"-kernel", kernelPath,
		"-initrd", initramfsPath,
		"-append", "console=ttyAMA0 rdinit=/smoke-init",
		"-netdev", "user,id=net0",
		"-device", "virtio-net-device,netdev=net0",
	}
	ctx, cancel := context.WithTimeout(t.Context(), vmSmokeTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "qemu-system-arm", args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("VM did not finish within %s\n%s", vmSmokeTimeout, output)
	}
	if err != nil {
		t.Fatalf("QEMU failed: %v\n%s", err, output)
	}
	return string(output)
}

func serialPayload(t *testing.T, output string) string {
	t.Helper()
	_, after, found := strings.Cut(output, smokeResponseBegin)
	if !found {
		t.Fatalf("serial output does not contain %q", smokeResponseBegin)
	}
	payload, _, found := strings.Cut(after, smokeResponseEnd)
	if !found {
		t.Fatalf("serial output does not contain %q after %q", smokeResponseEnd, smokeResponseBegin)
	}
	return strings.TrimSpace(payload)
}

func TestKoboVMReadeckAPISmoke(t *testing.T) {
	for _, command := range []string{"cpio", "docker", "qemu-system-arm"} {
		requireCommand(t, command)
	}

	// The VM test owns Readeck and waits for both its healthcheck and the loaded
	// API token before constructing or starting the guest.
	apiURL, token := startHostReadeckServer(t)

	kernelPath, baseInitramfsPath := ensureNetbootArtifacts(t)
	smokeInitramfsPath := buildSmokeInitramfs(t, baseInitramfsPath, apiURL, token)
	serialOutput := runKoboVM(t, kernelPath, smokeInitramfsPath)
	responseJSON := serialPayload(t, serialOutput)

	var bookmarks []json.RawMessage
	if err := json.Unmarshal([]byte(responseJSON), &bookmarks); err != nil {
		t.Fatalf("decode Readeck API response from VM: %v\n%s", err, responseJSON)
	}
	if bookmarks == nil {
		t.Fatal("Readeck bookmark API returned null instead of a JSON array")
	}
}
