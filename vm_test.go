// This file contains the host-side controller for the ARM system-VM release
// test. The idea is to attempt to recreate a physical installation:
//
//	Go test ── Docker CLI ── Readeck container
//	   │
//	   └── QEMU ARMv7 guest ── FAT32 /mnt/onboard
//	              │
//	              └── QEMU user networking ── 10.0.2.2 ── Readeck
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
	alpineRepositoryURL  = "http://dl-cdn.alpinelinux.org/alpine/v3.24/main"
	artifactCacheDir     = ".cache/kobodeck-testvm"
	qemuHostGatewayIP    = "10.0.2.2"
	vmTestTimeout        = 2 * time.Minute

	vmReadyMarker       = "KOBODECK_VM_READY"
	commandResultMarker = "KOBODECK_VM_COMMAND_DONE"

	readeckImage   = "codeberg.org/readeck/readeck:0.22.3"
	testAdminUser  = "testadmin"
	testAdminPass  = "testpass123"
	testAdminEmail = "testadmin@test.invalid"

	testArticleURL     = "https://example.com"
	testFavouriteShelf = "VM Favourites"

	testOnboardImageSize = 128 << 20 // 128 MiB
)

var bearerRegexp = regexp.MustCompile(`value="Authorization: Bearer ([^"]+)"`)

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
	hostURL string
	vmURL   string
	token   string
}

type vmTestOptions struct {
	createBookmark      bool
	archive             bool
	favouriteCollection string
	deleteLocal         bool
}

// defaultVMTestOptions returns the configuration used by normal release tests.
func defaultVMTestOptions() vmTestOptions {
	return vmTestOptions{
		createBookmark:      true,
		archive:             true,
		favouriteCollection: testFavouriteShelf,
	}
}

// docker runs the Docker CLI while keeping stdout separate from progress output
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

// startHostReadeckServer owns the complete external-service lifecycle.
func startHostReadeckServer(t *testing.T) *hostReadeckServer {
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

	// The CLI can create a user but cannot create an API token. User creation
	// therefore happens here; bootstrapToken completes the web-only second half.
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

	server := &hostReadeckServer{
		hostURL: "http://127.0.0.1:" + port,
		vmURL:   fmt.Sprintf("http://%s:%s", qemuHostGatewayIP, port),
	}
	server.token, err = bootstrapToken(server.hostURL)
	if err != nil {
		t.Fatalf("bootstrap Readeck API token: %v", err)
	}
	return server
}

// bootstrapToken uses Readeck's web session because Readeck 0.22.3 has no
// token-creation CLI command. The cookie jar reproduces the two browser steps:
// login, then submit the API-token form.
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

// apiRequest sends an authenticated host-side request to Readeck.
// It is used only to arrange fixtures and verify final server state; requests
// whose origin matters to the end-to-end behavior are issued by the ARM guest.
func (server *hostReadeckServer) apiRequest(
	t *testing.T,
	method, path string,
	body io.Reader,
) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, server.hostURL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+server.token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Readeck %s %s: %v", method, path, err)
	}
	return response
}

type testBookmarkState struct {
	Loaded     bool `json:"loaded"`
	IsArchived bool `json:"is_archived"`
	IsMarked   bool `json:"is_marked"`
}

// bookmarkState retrieves the Readeck fields asserted by the test for id.
func (server *hostReadeckServer) bookmarkState(t *testing.T, id string) testBookmarkState {
	t.Helper()
	response := server.apiRequest(t, http.MethodGet, "/api/bookmarks/"+id, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get Readeck bookmark %s: HTTP %s", id, response.Status)
	}
	var state testBookmarkState
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatalf("decode Readeck bookmark %s: %v", id, err)
	}
	return state
}

// createLoadedBookmark creates a Readeck bookmark and waits until its article
// has been fetched and converted, returning the bookmark ID.
func (server *hostReadeckServer) createLoadedBookmark(t *testing.T, articleURL string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"url": articleURL})
	if err != nil {
		t.Fatal(err)
	}
	response := server.apiRequest(t, http.MethodPost, "/api/bookmarks", bytes.NewReader(body))
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("create Readeck bookmark: HTTP %s", response.Status)
	}
	id := response.Header.Get("Bookmark-Id")
	if id == "" {
		t.Fatal("create Readeck bookmark: missing Bookmark-Id header")
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if server.bookmarkState(t, id).Loaded {
			return id
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("Readeck bookmark %s did not load within 30 seconds", id)
	return ""
}

// updateBookmark applies test fixture state to one Readeck bookmark.
func (server *hostReadeckServer) updateBookmark(
	t *testing.T,
	id string,
	fields map[string]bool,
) {
	t.Helper()
	body, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	response := server.apiRequest(
		t,
		http.MethodPatch,
		"/api/bookmarks/"+id,
		bytes.NewReader(body),
	)
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		t.Fatalf("update Readeck bookmark %s: HTTP %s", id, response.Status)
	}
}

// requireCommand fails immediately when a required host executable is absent.
func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("required command %q was not found: %v", name, err)
	}
}

// fileHasSHA256 reports whether path exists and has the expected SHA-256 hash.
func fileHasSHA256(path, wantHash string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)) == wantHash
}

// ensureNetbootArtifacts downloads each pinned Alpine artifact at most once.
func ensureNetbootArtifacts(t *testing.T) (kernelPath, initramfsPath string) {
	t.Helper()
	if err := os.MkdirAll(artifactCacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	for _, artifact := range alpineNetbootArtifacts {
		path := filepath.Join(artifactCacheDir, artifact.name)
		// A missing, partial, or stale cache entry follows the same download path.
		// There is no separate cache-cleanup operation for developers to remember.
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

// createOnboardDisk creates the storage Nickel exposes as /mnt/onboard.
func createOnboardDisk(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "onboard.fat")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(testOnboardImageSize); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := exec.CommandContext(t.Context(), "mkfs.vfat", path).CombinedOutput()
	if err != nil {
		t.Fatalf("format FAT32 onboard disk: %v\n%s", err, output)
	}
	return path
}

// buildVMInitramfs appends a gzip-compressed "newc" cpio archive to Alpine's
// initramfs. The overlay includes the release, its configuration, and the
// Nickel schema fixture needed by the VM test.
func buildVMInitramfs(
	t *testing.T,
	basePath, releasePath string,
	server *hostReadeckServer,
	options vmTestOptions,
) string {
	t.Helper()
	workDir := t.TempDir()
	overlayDir := filepath.Join(workDir, "overlay")
	testDir := filepath.Join(overlayDir, "test")
	apkDir := filepath.Join(overlayDir, "etc", "apk")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(apkDir, 0o755); err != nil {
		t.Fatal(err)
	}

	initScript, err := os.ReadFile("init_vm.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayDir, "init_vm.sh"), initScript, 0o755); err != nil {
		t.Fatal(err)
	}
	release, err := os.ReadFile(releasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(testDir, "KoboRoot.tgz"), release, 0o600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`[Server]
URL = %q
Token = %q
Timeout = 30

[Fetch]
Workers = 1
Limit = 0
Labels = ""
Status = ""

[Sync]
Archive = %t
FavouriteCollection = %q

[Log]
Verbose = false
Size = 1

[Output]
Path = "/mnt/onboard/kobodeck"
Delete = %t
`,
		server.vmURL,
		server.token,
		options.archive,
		options.favouriteCollection,
		options.deleteLocal,
	)
	if err := os.WriteFile(filepath.Join(testDir, "kobodeck.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	nickelSchema, err := os.ReadFile(filepath.Join("testdata", "nickel-schema-176.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(testDir, "nickel-schema.sql"),
		nickelSchema,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(apkDir, "repositories"),
		[]byte(alpineRepositoryURL+"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	combinedPath := filepath.Join(workDir, "initramfs-test")
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
		"etc/apk",
		"etc/apk/repositories",
		"test",
		"test/KoboRoot.tgz",
		"test/kobodeck.toml",
		"test/nickel-schema.sql",
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

// koboVM is a running QEMU process controlled through its serial console.
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

// startKoboVM starts QEMU, connects its serial pipes, and waits until the guest
// has configured networking and is ready to accept commands.
func startKoboVM(t *testing.T, kernelPath, initramfsPath, onboardPath string) *koboVM {
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
		"-drive", "file=" + onboardPath + ",format=raw,if=none,id=onboard",
		"-device", "virtio-blk-device,drive=onboard",
	}
	// testing.T cancels its Context before running cleanup functions. QEMU uses
	// its own bounded context so the registered cleanup still has an opportunity
	// to ask the guest to power off normally after a successful test.
	ctx, cancel := context.WithTimeout(context.Background(), vmTestTimeout)
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
	// Drain shutdown messages before Wait. Otherwise QEMU could block writing a
	// full pipe while Go waits for QEMU to exit.
	remaining, _ := io.ReadAll(vm.stdout)
	vm.transcript.Write(remaining)
	err := vm.command.Wait()
	vm.cancel()
	if err != nil && !vm.t.Failed() && vm.ctx.Err() != context.DeadlineExceeded {
		vm.t.Errorf("QEMU failed: %v\n%s\n%s", err, vm.transcript.String(), vm.stderr.String())
	}
}

const guestLogPath = "/mnt/onboard/.adds/kobodeck/kobodeck.log"

// logSince isolates one run's append-only log segment.
func logSince(t *testing.T, previous, current string) string {
	t.Helper()
	if !strings.HasPrefix(current, previous) {
		t.Fatal("Kobodeck log was unexpectedly replaced or truncated")
	}
	return current[len(previous):]
}

// requireLogContains fails unless logText contains every requested fragment.
func requireLogContains(t *testing.T, logText string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(logText, fragment) {
			t.Fatalf("Kobodeck log does not contain %q:\n%s", fragment, logText)
		}
	}
}

// requireCleanLog implements the README's "no errors" requirement.
func requireCleanLog(t *testing.T, logText string) {
	t.Helper()
	lower := strings.ToLower(logText)
	for _, fragment := range []string{"error", "failed", "panic"} {
		if strings.Contains(lower, fragment) {
			t.Fatalf("Kobodeck log contains %q:\n%s", fragment, logText)
		}
	}
}

type vmTestFixture struct {
	t            *testing.T
	vm           *koboVM
	server       *hostReadeckServer
	bookmarkID   string
	downloadPath string
}

// newVMTestFixture creates an isolated Readeck service, FAT32 disk, and ARM
// guest. It installs the release exactly as a Kobo would, but deliberately does
// not emit a Wi-Fi event; individual tests control when Kobodeck starts.
func newVMTestFixture(t *testing.T, options vmTestOptions) *vmTestFixture {
	t.Helper()
	for _, command := range []string{"cpio", "docker", "mkfs.vfat", "qemu-system-arm"} {
		requireCommand(t, command)
	}
	server := startHostReadeckServer(t)
	bookmarkID := ""
	if options.createBookmark {
		bookmarkID = server.createLoadedBookmark(t, testArticleURL)
	}
	releasePath := filepath.Join("build", "KoboRoot.tgz")
	kernelPath, baseInitramfsPath := ensureNetbootArtifacts(t)
	initramfsPath := buildVMInitramfs(
		t,
		baseInitramfsPath,
		releasePath,
		server,
		options,
	)
	onboardPath := createOnboardDisk(t)
	vm := startKoboVM(t, kernelPath, initramfsPath, onboardPath)
	vm.run(
		"modprobe -a virtio_blk fat vfat nls_cp437 nls_iso8859_1; " +
			"mdev -s; mkdir -p /mnt/onboard; mount -t vfat /dev/vda /mnt/onboard; " +
			"mount | grep -q ' on /mnt/onboard type vfat '",
	)
	vm.run("apk add --initdb eudev sqlite")
	vm.run(
		"tar -xzf /test/KoboRoot.tgz -C /; " +
			"mkdir -p /mnt/onboard/.adds/kobodeck /mnt/onboard/.kobo; " +
			"cp /test/kobodeck.toml /mnt/onboard/.adds/kobodeck/kobodeck.toml; " +
			"sqlite3 /mnt/onboard/.kobo/KoboReader.sqlite < /test/nickel-schema.sql; " +
			"touch /tmp/nickel-hardware-status",
	)
	vm.run(
		"ip link set eth0 down; ip link set eth0 name wlan0; ip link set wlan0 up; " +
			"mkdir -p /run/udev; udevd --daemon; udevadm control --reload-rules",
	)
	vm.run("[ ! -e " + guestLogPath + " ]")

	fixture := &vmTestFixture{
		t:          t,
		vm:         vm,
		server:     server,
		bookmarkID: bookmarkID,
	}
	if bookmarkID != "" {
		fixture.downloadPath = fmt.Sprintf(
			"/mnt/onboard/kobodeck/%s.kepub.epub",
			bookmarkID,
		)
	}
	return fixture
}

// guestLog returns the current Kobodeck log, or an empty string before the
// first invocation.
func (fixture *vmTestFixture) guestLog() string {
	fixture.t.Helper()
	return fixture.vm.run("[ ! -f " + guestLogPath + " ] || cat " + guestLogPath)
}

// triggerRemove emits a wlan0 remove event and verifies that it does not change
// the Kobodeck log.
func (fixture *vmTestFixture) triggerRemove() {
	fixture.t.Helper()
	before := fixture.guestLog()
	fixture.vm.run("udevadm trigger --action=remove /sys/class/net/wlan0; sleep 1")
	after := fixture.guestLog()
	if after != before {
		fixture.t.Fatalf("wlan0 remove event unexpectedly ran Kobodeck:\n%s", after)
	}
}

// triggerAdd emits a wlan0 add event, waits for exactly one Kobodeck process to
// complete, and returns only that process's log output.
func (fixture *vmTestFixture) triggerAdd() string {
	fixture.t.Helper()
	previous := fixture.guestLog()
	fixture.vm.run(
		"log=" + guestLogPath + "; " +
			"before=0; [ ! -f \"$log\" ] || before=$(grep -c ' completed in ' \"$log\"); " +
			"target=$((before + 1)); udevadm trigger --action=add /sys/class/net/wlan0; " +
			"waited=0; while :; do " +
			"current=0; [ ! -f \"$log\" ] || current=$(grep -c ' completed in ' \"$log\"); " +
			"[ \"$current\" -ge \"$target\" ] && break; " +
			"waited=$((waited + 1)); [ \"$waited\" -lt 60 ] || exit 1; sleep 1; " +
			"done; [ \"$current\" -eq \"$target\" ]",
	)
	return logSince(fixture.t, previous, fixture.guestLog())
}

// seedLocalArticle creates a non-empty KEPUB and the corresponding Nickel
// content row. Optionally it also places the article in the configured shelf.
func (fixture *vmTestFixture) seedLocalArticle(status bookStatus, inCollection bool) {
	fixture.t.Helper()
	contentID := "file://" + fixture.downloadPath
	shelfSQL := ""
	if inCollection {
		shelfSQL = fmt.Sprintf(
			"INSERT INTO Shelf (Id, InternalName, Name, _IsDeleted) "+
				"VALUES ('vm-favourites', 'vm-favourites', '%s', 'false'); "+
				"INSERT INTO ShelfContent (ShelfName, ContentId, _IsDeleted) "+
				"VALUES ('vm-favourites', '%s', 'false');",
			testFavouriteShelf,
			contentID,
		)
	}
	fixture.vm.run(fmt.Sprintf(
		"mkdir -p /mnt/onboard/kobodeck; "+
			"printf 'vm-fixture' > '%s'; "+
			"sqlite3 /mnt/onboard/.kobo/KoboReader.sqlite "+
			"\"INSERT INTO content "+
			"(ContentID, ContentType, MimeType, ReadStatus, ___UserID) "+
			"VALUES ('%s', 6, 'application/epub+zip', %d, 'vm-user'); %s\"",
		fixture.downloadPath,
		contentID,
		status,
		shelfSQL,
	))
}

// requireNoNickelRescan fails when Kobodeck emitted USB rescan events.
func (fixture *vmTestFixture) requireNoNickelRescan() {
	fixture.t.Helper()
	if events := fixture.vm.run("cat /tmp/nickel-hardware-status"); events != "" {
		fixture.t.Fatalf("unexpected Nickel rescan events:\n%s", events)
	}
}

// TestKoboVMWaitsForWiFi verifies the installed rule's event gating.
func TestKoboVMWaitsForWiFi(t *testing.T) {
	options := defaultVMTestOptions()
	options.createBookmark = false
	fixture := newVMTestFixture(t, options)

	fixture.triggerRemove()
	logText := fixture.triggerAdd()
	requireCleanLog(t, logText)
	requireLogContains(t, logText, "connecting to ", "completed in ")
	fixture.requireNoNickelRescan()
}

// TestKoboVMDownloadsBookmark verifies download, KEPUB conversion, FAT32
// storage, and Nickel rescan behavior using the real ARM release.
func TestKoboVMDownloadsBookmark(t *testing.T) {
	fixture := newVMTestFixture(t, defaultVMTestOptions())

	logText := fixture.triggerAdd()
	requireCleanLog(t, logText)
	requireLogContains(t, logText, "downloading ", "converted to ", "triggering Nickel rescan")
	fixture.vm.run("[ -s " + fixture.downloadPath + " ]")
	events := fixture.vm.run("cat /tmp/nickel-hardware-status")
	requireLogContains(t, events, "usb plug add", "usb plug remove")
}

// TestKoboVMSkipsExistingBookmark verifies that an existing non-empty KEPUB is
// neither replaced nor reported as a filesystem change.
func TestKoboVMSkipsExistingBookmark(t *testing.T) {
	fixture := newVMTestFixture(t, defaultVMTestOptions())
	fixture.seedLocalArticle(bookUnread, false)

	logText := fixture.triggerAdd()
	requireCleanLog(t, logText)
	for _, unwanted := range []string{"downloading ", "converted to ", "triggering Nickel rescan"} {
		if strings.Contains(logText, unwanted) {
			t.Fatalf("existing-book sync unexpectedly contains %q:\n%s", unwanted, logText)
		}
	}
	fixture.vm.run("[ \"$(cat '" + fixture.downloadPath + "')\" = vm-fixture ]")
	fixture.requireNoNickelRescan()
}

// TestKoboVMSyncsFinishedFavouriteBookmark verifies both state updates can
// occur in one reconciliation even though archiving removes the unread entry.
func TestKoboVMSyncsFinishedFavouriteBookmark(t *testing.T) {
	fixture := newVMTestFixture(t, defaultVMTestOptions())
	fixture.seedLocalArticle(bookRead, true)

	logText := fixture.triggerAdd()
	requireCleanLog(t, logText)
	requireLogContains(
		t,
		logText,
		"marking entry "+fixture.bookmarkID+" as archived",
		"marking entry "+fixture.bookmarkID+" as favourite",
	)
	state := fixture.server.bookmarkState(t, fixture.bookmarkID)
	if !state.IsArchived || !state.IsMarked {
		t.Fatalf("unexpected Readeck state: %+v", state)
	}
	fixture.requireNoNickelRescan()
}

// TestKoboVMUnfavouritesArchivedBookmark verifies that Kobo shelf membership
// remains authoritative after Readeck omits an archived entry from its feed.
func TestKoboVMUnfavouritesArchivedBookmark(t *testing.T) {
	fixture := newVMTestFixture(t, defaultVMTestOptions())
	fixture.server.updateBookmark(t, fixture.bookmarkID, map[string]bool{
		"is_archived": true,
		"is_marked":   true,
	})
	fixture.seedLocalArticle(bookUnread, false)

	logText := fixture.triggerAdd()
	requireCleanLog(t, logText)
	requireLogContains(t, logText, "unmarking entry "+fixture.bookmarkID+" as favourite")
	state := fixture.server.bookmarkState(t, fixture.bookmarkID)
	if !state.IsArchived || state.IsMarked {
		t.Fatalf("unexpected Readeck state: %+v", state)
	}
	fixture.requireNoNickelRescan()
}

// TestKoboVMDeletesStaleUnreadBookmark verifies optional cleanup and the
// resulting Nickel rescan for an article absent from Readeck's unread feed.
func TestKoboVMDeletesStaleUnreadBookmark(t *testing.T) {
	options := defaultVMTestOptions()
	options.deleteLocal = true
	fixture := newVMTestFixture(t, options)
	fixture.server.updateBookmark(t, fixture.bookmarkID, map[string]bool{"is_archived": true})
	fixture.seedLocalArticle(bookUnread, false)

	logText := fixture.triggerAdd()
	requireCleanLog(t, logText)
	requireLogContains(t, logText, "deleted "+fixture.downloadPath, "triggering Nickel rescan")
	fixture.vm.run("[ ! -e " + fixture.downloadPath + " ]")
	events := fixture.vm.run("cat /tmp/nickel-hardware-status")
	requireLogContains(t, events, "usb plug add", "usb plug remove")
}

// TestKoboVMKeepsStaleReadingBookmark verifies deletion never removes a book
// Nickel reports as currently being read.
func TestKoboVMKeepsStaleReadingBookmark(t *testing.T) {
	options := defaultVMTestOptions()
	options.deleteLocal = true
	fixture := newVMTestFixture(t, options)
	fixture.server.updateBookmark(t, fixture.bookmarkID, map[string]bool{"is_archived": true})
	fixture.seedLocalArticle(bookReading, false)

	logText := fixture.triggerAdd()
	requireCleanLog(t, logText)
	requireLogContains(t, logText, "not deleting book currently being read: "+fixture.downloadPath)
	fixture.vm.run("[ -s " + fixture.downloadPath + " ]")
	fixture.requireNoNickelRescan()
}

// TestKoboVMRejectsDuplicateLaunch verifies the process lock by starting two
// copies of the installed ARM binary concurrently. eudev serializes events for
// one device, so direct concurrent execution makes the lock race deterministic.
func TestKoboVMRejectsDuplicateLaunch(t *testing.T) {
	fixture := newVMTestFixture(t, defaultVMTestOptions())
	previous := fixture.guestLog()
	fixture.vm.run(
		"/usr/local/bin/kobodeck & first_pid=$!; " +
			"waited=0; while [ ! -e /tmp/kobodeck.lock ]; do " +
			"waited=$((waited + 1)); [ \"$waited\" -lt 20 ] || break; sleep 1; " +
			"done; [ -e /tmp/kobodeck.lock ]; sleep 1; " +
			"/usr/local/bin/kobodeck; duplicate_status=$?; " +
			"wait \"$first_pid\"; first_status=$?; " +
			"[ \"$first_status\" -eq 0 ] && [ \"$duplicate_status\" -ne 0 ]",
	)
	logText := logSince(t, previous, fixture.guestLog())
	requireCleanLog(t, logText)
	requireLogContains(t, logText, "already running", "downloading ", "completed in ")
	if count := strings.Count(logText, "downloading "); count != 1 {
		t.Fatalf("duplicate launch produced %d downloads:\n%s", count, logText)
	}
	if count := strings.Count(logText, " completed in "); count != 1 {
		t.Fatalf("duplicate launch produced %d completed runs:\n%s", count, logText)
	}
}

// TestKoboVMCheckCommand verifies the release binary's manual configuration
// check reaches Readeck without changing files or Nickel state.
func TestKoboVMCheckCommand(t *testing.T) {
	fixture := newVMTestFixture(t, defaultVMTestOptions())

	output := fixture.vm.run(
		"/usr/local/bin/kobodeck " +
			"--config /mnt/onboard/.adds/kobodeck/kobodeck.toml --check",
	)
	requireLogContains(t, output, "Configuration:", "Connecting to Readeck... OK")
	requireLogContains(t, output, fixture.bookmarkID, "1 bookmarks to sync")
	fixture.vm.run("[ ! -e " + fixture.downloadPath + " ]")
	fixture.requireNoNickelRescan()
}

// TestKoboVMUninstallsFromEmptyConfig verifies the documented uninstall path
// against files installed from the release tarball.
func TestKoboVMUninstallsFromEmptyConfig(t *testing.T) {
	fixture := newVMTestFixture(t, defaultVMTestOptions())

	fixture.vm.run(
		": > /mnt/onboard/.adds/kobodeck/kobodeck.toml; " +
			"/usr/local/bin/kobodeck; " +
			"[ ! -e /usr/local/bin/kobodeck ]; " +
			"[ ! -e /etc/udev/rules.d/90-kobodeck.rules ]; " +
			"[ ! -e /mnt/onboard/.adds/kobodeck ]",
	)
}
