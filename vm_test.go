//go:build vmtest

// This file contains the host-side controller for the minimal ARM system-VM
// smoke test.
//
// Readeck still runs outside the guest in a host-side container owned directly
// by TestKoboVMReadeckAPISmoke. The simulated Kobo is built entirely from
// Alpine's official, pinned ARMv7 netboot kernel and initramfs. A tiny second
// initramfs archive supplies testvm/smoke-init and the per-test Readeck
// URL/token.
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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
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

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	alpineNetbootBaseURL = "https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/armv7/netboot-3.24.1"
	qemuHostGatewayIP    = "10.0.2.2"
	vmSmokeTimeout       = 60 * time.Second

	smokeResponseBegin = "KOBODECK_API_RESPONSE_BEGIN"
	smokeResponseEnd   = "KOBODECK_API_RESPONSE_END"

	readeckImage    = "codeberg.org/readeck/readeck:latest"
	testAdminUser   = "testadmin"
	testAdminPass   = "testpass123"
	testAdminEmail  = "testadmin@test.invalid"
	testBookmarkURL = "https://example.com"
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

type hostReadeckServer struct {
	container testcontainers.Container
	baseURL   string
	token     string
}

type koboVM struct {
	t          *testing.T
	serialPath string
	cmd        *exec.Cmd
	done       chan error
	exited     chan struct{}
}

// startHostReadeckServer owns the complete external-service lifecycle for this
// VM test. Testcontainers does not return until Readeck's own healthcheck
// succeeds, so no guest work can race an unready server.
func startHostReadeckServer(t *testing.T) *hostReadeckServer {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.Run(ctx, readeckImage,
		testcontainers.WithWaitStrategy(
			wait.ForExec([]string{"readeck", "healthcheck"}).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start host-side Readeck: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Errorf("terminate host-side Readeck: %v", err)
		}
	})

	exitCode, _, err := container.Exec(ctx, []string{
		"readeck", "user",
		"-user", testAdminUser,
		"-password", testAdminPass,
		"-email", testAdminEmail,
	})
	if err != nil {
		t.Fatalf("create Readeck admin user: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("create Readeck admin user: exited with status %d", exitCode)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("get Readeck container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "8000/tcp")
	if err != nil {
		t.Fatalf("get Readeck mapped port: %v", err)
	}

	server := &hostReadeckServer{
		container: container,
		baseURL:   fmt.Sprintf("http://%s:%s", host, port.Port()),
	}
	server.token, err = bootstrapToken(server.baseURL)
	if err != nil {
		t.Fatalf("bootstrap Readeck API token: %v", err)
	}
	return server
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

func (server *hostReadeckServer) apiRequest(t *testing.T, method, path string, body io.Reader) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, server.baseURL+path, body)
	if err != nil {
		t.Fatalf("build Readeck request %s %s: %v", method, path, err)
	}
	request.Header.Set("Authorization", "Bearer "+server.token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send Readeck request %s %s: %v", method, path, err)
	}
	return response
}

// createLoadedBookmark does not return until Readeck has fetched and parsed the
// article. This is the final readiness gate before the VM is started.
func (server *hostReadeckServer) createLoadedBookmark(t *testing.T, bookmarkURL string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"url": bookmarkURL})
	if err != nil {
		t.Fatal(err)
	}
	response := server.apiRequest(t, http.MethodPost, "/api/bookmarks", bytes.NewReader(body))
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("create Readeck bookmark: expected 202, got %d", response.StatusCode)
	}
	id := response.Header.Get("Bookmark-Id")
	if id == "" {
		t.Fatal("create Readeck bookmark: missing Bookmark-Id header")
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		response = server.apiRequest(t, http.MethodGet, "/api/bookmarks/"+id, nil)
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			t.Fatalf("load Readeck bookmark %s: expected 200, got %d", id, response.StatusCode)
		}
		var bookmark struct {
			Loaded bool `json:"loaded"`
		}
		err := json.NewDecoder(response.Body).Decode(&bookmark)
		response.Body.Close()
		if err != nil {
			t.Fatalf("decode Readeck bookmark %s: %v", id, err)
		}
		if bookmark.Loaded {
			t.Cleanup(func() {
				response := server.apiRequest(t, http.MethodDelete, "/api/bookmarks/"+id, nil)
				response.Body.Close()
			})
			return id
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("Readeck bookmark %s did not finish loading within 30s", id)
	return ""
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("required command %q was not found: %v", name, err)
	}
}

func artifactCacheDir(t *testing.T) string {
	t.Helper()
	if cacheDir := os.Getenv("KOBODECK_VM_CACHE_DIR"); cacheDir != "" {
		return cacheDir
	}
	return filepath.Join(".cache", "kobodeck-testvm")
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ensureNetbootArtifacts downloads each pinned Alpine artifact at most once.
// Downloads use a temporary file and atomic rename so an interrupted run never
// leaves a partially valid-looking cache entry.
func ensureNetbootArtifacts(t *testing.T) (kernelPath, initramfsPath string) {
	t.Helper()
	cacheDir := artifactCacheDir(t)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	paths := make(map[string]string, len(alpineNetbootArtifacts))
	for _, artifact := range alpineNetbootArtifacts {
		path := filepath.Join(cacheDir, artifact.name)
		if sum, err := fileSHA256(path); err == nil && sum == artifact.sha256 {
			paths[artifact.name] = path
			continue
		}

		requestURL := alpineNetbootBaseURL + "/" + artifact.name
		resp, err := client.Get(requestURL)
		if err != nil {
			t.Fatalf("download %s: %v", requestURL, err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Fatalf("download %s: got HTTP %s", requestURL, resp.Status)
		}

		temp, err := os.CreateTemp(cacheDir, artifact.name+".partial-*")
		if err != nil {
			resp.Body.Close()
			t.Fatal(err)
		}
		tempPath := temp.Name()
		if _, err = io.Copy(temp, resp.Body); err == nil {
			err = temp.Close()
		} else {
			_ = temp.Close()
		}
		resp.Body.Close()
		if err != nil {
			_ = os.Remove(tempPath)
			t.Fatalf("save %s: %v", requestURL, err)
		}

		sum, err := fileSHA256(tempPath)
		if err != nil {
			_ = os.Remove(tempPath)
			t.Fatal(err)
		}
		if sum != artifact.sha256 {
			_ = os.Remove(tempPath)
			t.Fatalf("verify %s: got sha256 %s, want %s", requestURL, sum, artifact.sha256)
		}
		if err := os.Rename(tempPath, path); err != nil {
			_ = os.Remove(tempPath)
			t.Fatal(err)
		}
		paths[artifact.name] = path
	}

	return paths["vmlinuz-lts"], paths["initramfs-lts"]
}

func copyFile(destination string, source io.Reader) error {
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, source); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
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
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	initSource, err := os.Open("testvm/smoke-init")
	if err != nil {
		t.Fatal(err)
	}
	if err := copyFile(filepath.Join(overlayDir, "smoke-init"), initSource); err != nil {
		initSource.Close()
		t.Fatal(err)
	}
	initSource.Close()
	if err := os.Chmod(filepath.Join(overlayDir, "smoke-init"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "url"), []byte(apiURL+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "token"), []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	combinedPath := filepath.Join(workDir, "initramfs-smoke")
	combined, err := os.Create(combinedPath)
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.Open(basePath)
	if err != nil {
		combined.Close()
		t.Fatal(err)
	}
	if _, err := io.Copy(combined, base); err != nil {
		base.Close()
		combined.Close()
		t.Fatal(err)
	}
	base.Close()

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

func startKoboVM(t *testing.T, kernelPath, initramfsPath string) *koboVM {
	t.Helper()
	serialPath := filepath.Join(t.TempDir(), "serial.log")
	serial, err := os.Create(serialPath)
	if err != nil {
		t.Fatal(err)
	}

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
	cmd := exec.Command("qemu-system-arm", args...)
	cmd.Stdout = serial
	cmd.Stderr = serial
	if err := cmd.Start(); err != nil {
		serial.Close()
		t.Fatal(err)
	}

	vm := &koboVM{
		t:          t,
		serialPath: serialPath,
		cmd:        cmd,
		done:       make(chan error, 1),
		exited:     make(chan struct{}),
	}
	go func() {
		vm.done <- cmd.Wait()
		close(vm.exited)
	}()

	t.Cleanup(func() {
		select {
		case <-vm.exited:
		default:
			_ = vm.cmd.Process.Signal(os.Interrupt)
		}
		select {
		case <-vm.done:
		case <-time.After(5 * time.Second):
			select {
			case <-vm.exited:
			default:
				_ = vm.cmd.Process.Kill()
			}
			<-vm.done
		}
		_ = serial.Close()
		if t.Failed() {
			if output, err := os.ReadFile(serialPath); err == nil {
				t.Logf("QEMU serial console:\n%s", output)
			}
		}
	})
	return vm
}

func (vm *koboVM) waitForSerial(marker string) string {
	vm.t.Helper()
	deadline := time.Now().Add(vmSmokeTimeout)
	for time.Now().Before(deadline) {
		output, err := os.ReadFile(vm.serialPath)
		if err != nil {
			vm.t.Fatal(err)
		}
		if strings.Contains(string(output), marker) {
			return string(output)
		}
		select {
		case <-vm.exited:
			vm.t.Fatalf("QEMU exited before emitting %q", marker)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	vm.t.Fatalf("VM did not emit %q within %s", marker, vmSmokeTimeout)
	return ""
}

func readeckURLFromVM(t *testing.T, serverURL string) string {
	t.Helper()
	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(parsedURL.Host)
	if err != nil {
		t.Fatalf("parse Readeck address %q: %v", serverURL, err)
	}
	return "http://" + net.JoinHostPort(qemuHostGatewayIP, port)
}

func serialPayload(t *testing.T, output, begin, end string) string {
	t.Helper()
	start := strings.Index(output, begin)
	if start < 0 {
		t.Fatalf("serial output does not contain %q", begin)
	}
	start += len(begin)
	finish := strings.Index(output[start:], end)
	if finish < 0 {
		t.Fatalf("serial output does not contain %q after %q", end, begin)
	}
	return strings.TrimSpace(output[start : start+finish])
}

func TestKoboVMReadeckAPISmoke(t *testing.T) {
	for _, command := range []string{"cpio", "qemu-system-arm"} {
		requireCommand(t, command)
	}

	// The VM test owns Readeck and waits for both its healthcheck and the loaded
	// bookmark before constructing or starting the guest.
	server := startHostReadeckServer(t)
	bookmarkID := server.createLoadedBookmark(t, testBookmarkURL)
	apiURL := readeckURLFromVM(t, server.baseURL) + "/api/bookmarks/" + bookmarkID

	kernelPath, baseInitramfsPath := ensureNetbootArtifacts(t)
	smokeInitramfsPath := buildSmokeInitramfs(
		t,
		baseInitramfsPath,
		apiURL,
		server.token,
	)
	vm := startKoboVM(t, kernelPath, smokeInitramfsPath)
	serialOutput := vm.waitForSerial(smokeResponseEnd)
	responseJSON := serialPayload(t, serialOutput, smokeResponseBegin, smokeResponseEnd)

	var bookmark struct {
		URL    string `json:"url"`
		Loaded bool   `json:"loaded"`
	}
	if err := json.Unmarshal([]byte(responseJSON), &bookmark); err != nil {
		t.Fatalf("decode Readeck API response from VM: %v\n%s", err, responseJSON)
	}
	if bookmark.URL != testBookmarkURL || !bookmark.Loaded {
		t.Fatalf("unexpected Readeck API response from VM: %+v", bookmark)
	}
}
