// This file contains the host-side controller for the minimal ARM system-VM
// smoke test.
//
// Readeck still runs outside the guest in a host-side container owned directly
// by TestKoboVMReadeckAPISmoke. The test uses the Docker CLI because managing
// one disposable container does not justify a container SDK and its dependency
// tree. The simulated Kobo is built entirely from Alpine's official, pinned
// ARMv7 netboot kernel and initramfs. A tiny second initramfs archive supplies
// init_vm.sh and the per-test Readeck URL/token.
//
// There is intentionally no guest root disk, Docker-built ARM image, FAT
// volume, eudev, or SSH server in this smoke test. Those belong in later tests
// that actually need Kobo storage and udev behavior.
package main

import (
	"bufio"
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
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	alpineNetbootBaseURL = "https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/armv7/netboot-3.24.1"
	artifactCacheDir     = ".cache/kobodeck-testvm"
	qemuHostGatewayIP    = "10.0.2.2"
	vmSmokeTimeout       = 60 * time.Second

	vmReadyMarker       = "KOBODECK_VM_READY"
	commandResultMarker = "KOBODECK_VM_COMMAND_DONE"

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

// buildVMInitramfs appends a gzip-compressed "newc" cpio archive to Alpine's
// initramfs. Linux supports a sequence of compressed cpio members, so the
// official archive remains byte-for-byte unchanged and our overlay adds only:
//
//   - /init_vm.sh
//   - /etc/kobodeck-smoke/url
//   - /etc/kobodeck-smoke/token
func buildVMInitramfs(t *testing.T, basePath, apiURL, token string) string {
	t.Helper()
	workDir := t.TempDir()
	overlayDir := filepath.Join(workDir, "overlay")
	configDir := filepath.Join(overlayDir, "etc", "kobodeck-smoke")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	initScript, err := os.ReadFile("init_vm.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayDir, "init_vm.sh"), initScript, 0o755); err != nil {
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
		"init_vm.sh",
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

// koboVM is a running QEMU process controlled through its serial console. The
// guest init does no test-specific work; run sends that work from the Go test.
type koboVM struct {
	t          *testing.T
	ctx        context.Context
	cancel     context.CancelFunc
	command    *exec.Cmd
	stdin      io.WriteCloser
	stdout     *bufio.Reader
	stderr     bytes.Buffer
	transcript strings.Builder
	nextMarker int
	ready      bool
	closed     bool
}

func startKoboVM(t *testing.T, kernelPath, initramfsPath string) *koboVM {
	t.Helper()
	args := []string{
		"-machine", "virt",
		"-cpu", "cortex-a15",
		"-smp", "1",
		"-m", "256",
		"-display", "none",
		"-monitor", "none",
		"-serial", "stdio",
		"-no-reboot",
		"-kernel", kernelPath,
		"-initrd", initramfsPath,
		"-append", "console=ttyAMA0 rdinit=/init_vm.sh",
		"-netdev", "user,id=net0",
		"-device", "virtio-net-device,netdev=net0",
	}
	ctx, cancel := context.WithTimeout(t.Context(), vmSmokeTimeout)
	command := exec.CommandContext(ctx, "qemu-system-arm", args...)
	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	vm := &koboVM{
		t:       t,
		ctx:     ctx,
		cancel:  cancel,
		command: command,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
	}
	command.Stderr = &vm.stderr
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("start QEMU: %v\n%s", err, vm.stderr.String())
	}
	t.Cleanup(vm.shutdown)

	vm.readUntil(vmReadyMarker)
	vm.ready = true
	return vm
}

// readUntil consumes serial output through marker and returns everything before
// it plus the remainder of the marker's line.
func (vm *koboVM) readUntil(marker string) (string, string) {
	vm.t.Helper()
	var output strings.Builder
	for {
		line, err := vm.stdout.ReadString('\n')
		vm.transcript.WriteString(line)
		if index := strings.Index(line, marker); index >= 0 {
			output.WriteString(line[:index])
			return output.String(), strings.TrimSpace(line[index+len(marker):])
		}
		output.WriteString(line)
		if err != nil {
			vm.t.Fatalf("read VM serial output before %q: %v\n%s\n%s",
				marker, err, vm.transcript.String(), vm.stderr.String())
		}
	}
}

// run executes one shell command in the guest and returns its output. A unique
// marker carries the exit status back over the same serial stream.
func (vm *koboVM) run(command string) string {
	vm.t.Helper()
	vm.nextMarker++
	marker := fmt.Sprintf("%s_%d", commandResultMarker, vm.nextMarker)
	wireCommand := fmt.Sprintf(
		"{ %s; }; status=$?; printf '\\n%s:%%s\\n' \"$status\"\n",
		command,
		marker,
	)
	if _, err := io.WriteString(vm.stdin, wireCommand); err != nil {
		vm.t.Fatalf("send VM command: %v", err)
	}
	output, statusText := vm.readUntil(marker + ":")
	status, err := strconv.Atoi(statusText)
	if err != nil {
		vm.t.Fatalf("parse VM command status %q: %v", statusText, err)
	}
	if status != 0 {
		vm.t.Fatalf("VM command exited with status %d: %s\n%s", status, command, output)
	}
	return strings.TrimSpace(output)
}

// shutdown asks the command shell to exit. init_vm.sh then powers off the guest,
// which lets QEMU terminate normally. If boot never reached the ready marker,
// cleanup kills QEMU instead because no command shell is available.
func (vm *koboVM) shutdown() {
	vm.t.Helper()
	if vm.closed {
		return
	}
	vm.closed = true

	if vm.ready {
		_, _ = io.WriteString(vm.stdin, "exit\n")
	} else if vm.command.Process != nil {
		_ = vm.command.Process.Kill()
	}
	_ = vm.stdin.Close()
	remaining, _ := io.ReadAll(vm.stdout)
	vm.transcript.Write(remaining)
	err := vm.command.Wait()
	vm.cancel()
	if err != nil && vm.ctx.Err() != context.DeadlineExceeded {
		vm.t.Errorf("QEMU failed: %v\n%s\n%s", err, vm.transcript.String(), vm.stderr.String())
	}
}

func TestKoboVMReadeckAPISmoke(t *testing.T) {
	for _, command := range []string{"cpio", "docker", "qemu-system-arm"} {
		requireCommand(t, command)
	}

	// The VM test owns Readeck and waits for both its healthcheck and the loaded
	// API token before constructing or starting the guest.
	apiURL, token := startHostReadeckServer(t)

	kernelPath, baseInitramfsPath := ensureNetbootArtifacts(t)
	initramfsPath := buildVMInitramfs(t, baseInitramfsPath, apiURL, token)
	vm := startKoboVM(t, kernelPath, initramfsPath)
	responseJSON := vm.run(
		`api_url=$(cat /etc/kobodeck-smoke/url); ` +
			`token=$(cat /etc/kobodeck-smoke/token); ` +
			`wget -qO- --header="Authorization: Bearer $token" "$api_url"`,
	)

	var bookmarks []json.RawMessage
	if err := json.Unmarshal([]byte(responseJSON), &bookmarks); err != nil {
		t.Fatalf("decode Readeck API response from VM: %v\n%s", err, responseJSON)
	}
	if bookmarks == nil {
		t.Fatal("Readeck bookmark API returned null instead of a JSON array")
	}
	vm.shutdown()
}
