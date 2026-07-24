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
		// Readeck's own command checks more than an open TCP socket. Waiting on it
		// is preferable to sleeping for an arbitrary startup duration.
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

	// Ask Docker for the random host port rather than inspecting internal
	// container networking, which is intentionally irrelevant to the guest.
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
// login, then submit the API-token form. Parsing HTML is less attractive than a
// public API, which is why the image is pinned and failure is explicit.
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

func (server *hostReadeckServer) apiRequest(
	t *testing.T,
	method, path string,
	body io.Reader,
) *http.Response {
	t.Helper()
	// Host-side API calls arrange test state and verify results. The behavior
	// under test still originates in the ARM guest through server.vmURL.
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

// Only fields asserted by this test are decoded. Depending on Readeck's full
// response schema would make harmless upstream additions irrelevant noise.
type testBookmarkState struct {
	Loaded     bool `json:"loaded"`
	IsArchived bool `json:"is_archived"`
	IsMarked   bool `json:"is_marked"`
}

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

	// Bookmark creation is asynchronous: HTTP 202 means queued, not ready for an
	// EPUB download. Poll the resource's Loaded flag before booting the VM.
	// Waiting here keeps later guest failures attributable to Kobodeck itself.
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

func requireCommand(t *testing.T, name string) {
	t.Helper()
	// Fail at the start with a useful prerequisite message instead of much later
	// with exec.ErrNotFound after Readeck and temporary artifacts already exist.
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("required command %q was not found: %v", name, err)
	}
}

func fileHasSHA256(path, wantHash string) bool {
	// The two netboot files total only a few megabytes, so reading them into
	// memory is simpler than maintaining streaming hash and temporary-file code.
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)) == wantHash
}

// ensureNetbootArtifacts downloads each pinned Alpine artifact at most once.
// The files are small enough to verify in memory. If a download is interrupted,
// its hash fails and the next run simply downloads it again.
//
// Downloading official netboot artifacts avoids maintaining a custom VM image.
// Hash pinning retains reproducibility and prevents a mirror-side replacement
// from silently changing the kernel/userspace being tested.
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

// buildReleaseTarball intentionally invokes the public release target instead
// of compiling an ARM binary directly in the test. This covers the packaging
// paths, permissions, udev rule, cross-compilation flags, and exact archive that
// users install. Reimplementing tarball creation here would test a parallel
// artifact rather than the release.
func buildReleaseTarball(t *testing.T) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "make", "tarball")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build release tarball: %v\n%s", err, output)
	}
	path, err := filepath.Abs(filepath.Join("build", "KoboRoot.tgz"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// createOnboardDisk creates the storage Nickel exposes as /mnt/onboard.
//
// A host directory shared with 9p/virtiofs would be easier to inspect, but it
// would use Linux filesystem semantics. FAT32 catches the actual filename,
// permission, and rename behavior seen by Kobodeck on a physical device.
func createOnboardDisk(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "onboard.fat")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Truncate normally creates a sparse file, so the nominal 128 MiB capacity
	// does not consume 128 MiB of host storage up front.
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
// initramfs. Linux supports a sequence of compressed cpio members, so the
// official archive remains byte-for-byte unchanged. The overlay carries the
// VM controller, the real release tarball, its test configuration, and the
// Alpine repository used to install eudev and sqlite in the guest.
//
// Appending an overlay is substantially smaller than constructing a root disk:
// the kernel unpacks both cpio members into one writable root. Credentials and
// release artifacts are per-test data, so checking a prepared initramfs into
// the repository would be both stale and unsafe.
func buildVMInitramfs(
	t *testing.T,
	basePath, releasePath string,
	server *hostReadeckServer,
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

	// Keep the script as a normal repository file so shellcheck/readers can
	// inspect it directly; embedding its source as a Go string would obscure the
	// guest side of the protocol.
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
	// This is the same on-device configuration path and format used by a real
	// installation. One worker keeps log ordering deterministic; Delete=false
	// ensures archiving does not remove the file whose persistence is asserted.
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
Archive = true
FavouriteCollection = %q

[Log]
Verbose = false
Size = 1

[Output]
Path = "/mnt/onboard/kobodeck"
Delete = false
`, server.vmURL, server.token, testFavouriteShelf)
	if err := os.WriteFile(filepath.Join(testDir, "kobodeck.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	// The netboot initramfs includes apk and signing keys, but not repository
	// configuration. eudev is installed at runtime because bundling it and all
	// shared-library dependencies manually would recreate an image builder.
	if err := os.WriteFile(
		filepath.Join(apkDir, "repositories"),
		[]byte(alpineRepositoryURL+"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	combinedPath := filepath.Join(workDir, "initramfs-test")
	// Copy the upstream initramfs exactly, then append our compressed member.
	// Linux supports concatenated initramfs archives and processes them in order.
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
	// "newc" is the portable initramfs cpio format understood by Linux. Listing
	// files explicitly makes it impossible for an unrelated temporary file to
	// leak into the guest; using `find | cpio` would be shorter but less precise.
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
//
// The guest init does no test-specific work; run sends all installation,
// mutation, and assertion commands from Go. SSH would provide a richer control
// protocol, but would require a daemon, host keys, authentication, another
// guest package, port forwarding, and a second readiness check. The serial
// console already exists for kernel diagnostics, so a tiny marker protocol is
// sufficient and has fewer failure modes.
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

func startKoboVM(t *testing.T, kernelPath, initramfsPath, onboardPath string) *koboVM {
	t.Helper()
	// "virt" is QEMU's generic ARM platform rather than a specific Kobo board.
	// cortex-a15 guarantees ARMv7 execution, which is the important release
	// property. One CPU and 256 MiB keep emulation inexpensive while leaving room
	// for the in-memory root, APK installation, and the static Kobodeck binary.
	args := []string{
		"-machine", "virt",
		"-cpu", "cortex-a15",
		"-smp", "1",
		"-m", "256",
		// Give stdio exclusively to the PL011 serial port. -nographic also
		// multiplexes QEMU's monitor onto stdio, making programmatic commands and
		// Ctrl-A escape handling unnecessarily fragile.
		"-display", "none",
		"-monitor", "none",
		"-serial", "stdio",
		"-no-reboot",
		"-kernel", kernelPath,
		"-initrd", initramfsPath,
		// rdinit selects our small test PID 1 instead of Alpine's installer init.
		"-append", "console=ttyAMA0 rdinit=/init_vm.sh",

		// User networking requires no root privileges. The explicit MMIO virtio
		// device is used because QEMU's compact -nic form does not accept
		// virtio-net-device on this ARM machine.
		"-netdev", "user,id=net0",
		"-device", "virtio-net-device,netdev=net0",

		// Expose the raw FAT image as a native ARM virtio block device. Mounting
		// happens later from Go so reaching READY does not depend on test-specific
		// storage setup.
		"-drive", "file=" + onboardPath + ",format=raw,if=none,id=onboard",
		"-device", "virtio-blk-device,drive=onboard",
	}
	ctx, cancel := context.WithTimeout(t.Context(), vmTestTimeout)
	command := exec.CommandContext(ctx, "qemu-system-arm", args...)
	// Separate pipes allow Go to issue commands while continuously consuming the
	// same serial output that carries boot logs and result markers.
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
	// Register cleanup before waiting for READY. If the guest fails during boot,
	// testing.T still kills QEMU and releases the FAT image/container lifecycle.
	t.Cleanup(vm.shutdown)

	// READY is printed only after init_vm.sh has DHCP connectivity and has
	// entered its command loop. No arbitrary boot sleep is necessary.
	vm.readUntil(vmReadyMarker)
	vm.ready = true
	return vm
}

// readUntil consumes serial output through marker and returns everything before
// it plus the remainder of the marker's line.
//
// Reading line-by-line avoids bufio.Scanner's 64 KiB token limit: a Readeck
// response or verbose guest command may legitimately emit a long line. The
// complete transcript is retained so an early EOF includes all kernel context.
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
//
// Commands are wrapped in one compound shell line. Sending the status marker in
// a later line would lose `$?` when the command loop performs its next read.
// Incrementing the marker prevents output from an earlier command from
// satisfying a later wait. init_vm.sh disables tty echo, so the marker embedded
// in the input itself is not mistaken for the emitted completion marker.
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
//
// A normal guest poweroff is preferable to Process.Kill: it proves PID 1 owns a
// complete lifecycle and lets QEMU flush the FAT-backed block device. Process
// kill remains the only safe fallback before the command loop exists.
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

// runKobodeckSync simulates a Wi-Fi connection by sending an actual udev event.
// It intentionally does not invoke /usr/local/bin/kobodeck directly: launching
// through the rule installed by KoboRoot.tgz tests both packaging and the
// production activation path.
//
// The udev rule backgrounds Kobodeck, so udevadm returning does not mean a sync
// has completed. Count the binary's final "completed in" log records before the
// event and poll until exactly a newer process has finished. This is a
// condition-based wait rather than a fixed sleep, keeping fast runs fast while
// still tolerating the first run's EPUB conversion and Nickel-rescan delay.
func runKobodeckSync(vm *koboVM, reconnect bool) string {
	vm.t.Helper()
	remove := ""
	if reconnect {
		// ACTION=remove must not match the installed ACTION=="add" rule. Waiting
		// one second leaves enough time for an accidental remove-triggered launch
		// to alter the baseline count and make the test fail deterministically.
		remove = "udevadm trigger --action=remove /sys/class/net/wlan0; sleep 1; "
	}
	vm.run(
		remove +
			"log=" + guestLogPath + "; " +
			"before=0; [ ! -f \"$log\" ] || before=$(grep -c ' completed in ' \"$log\"); " +
			"udevadm trigger --action=add /sys/class/net/wlan0; " +
			"waited=0; while :; do " +
			"current=0; [ ! -f \"$log\" ] || current=$(grep -c ' completed in ' \"$log\"); " +
			"[ \"$current\" -gt \"$before\" ] && break; " +
			"waited=$((waited + 1)); [ \"$waited\" -lt 60 ] || exit 1; sleep 1; " +
			"done",
	)
	return vm.run("cat " + guestLogPath)
}

// logSince isolates one connection's append-only log segment. Kobodeck uses a
// rotating logger, but this three-run test is far below the configured 1 MiB
// threshold. A missing prefix therefore indicates unexpected truncation rather
// than legitimate rotation.
func logSince(t *testing.T, previous, current string) string {
	t.Helper()
	if !strings.HasPrefix(current, previous) {
		t.Fatal("Kobodeck log was unexpectedly replaced or truncated")
	}
	return current[len(previous):]
}

func requireLogContains(t *testing.T, logText string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(logText, fragment) {
			t.Fatalf("Kobodeck log does not contain %q:\n%s", fragment, logText)
		}
	}
}

// requireCleanLog implements the README's "no errors" requirement. Checking a
// few strong failure terms is less brittle than asserting the entire log text,
// which contains timestamps, durations, IDs, and upstream response details.
func requireCleanLog(t *testing.T, logText string) {
	t.Helper()
	lower := strings.ToLower(logText)
	for _, fragment := range []string{"error", "failed", "panic"} {
		if strings.Contains(lower, fragment) {
			t.Fatalf("Kobodeck log contains %q:\n%s", fragment, logText)
		}
	}
}

func TestKoboVMEndToEnd(t *testing.T) {
	// Check every host prerequisite before starting Docker or downloading/building
	// anything. sqlite runs inside the guest and is installed through apk.
	for _, command := range []string{"cpio", "docker", "make", "mkfs.vfat", "qemu-system-arm"} {
		requireCommand(t, command)
	}

	// Arrange all external and immutable inputs before boot. Readeck must finish
	// extracting the article before Kobodeck can request article.epub. Building
	// the real tarball here also makes a packaging failure fail the release test.
	server := startHostReadeckServer(t)
	bookmarkID := server.createLoadedBookmark(t, testArticleURL)
	releasePath := buildReleaseTarball(t)
	kernelPath, baseInitramfsPath := ensureNetbootArtifacts(t)
	initramfsPath := buildVMInitramfs(t, baseInitramfsPath, releasePath, server)
	onboardPath := createOnboardDisk(t)
	vm := startKoboVM(t, kernelPath, initramfsPath, onboardPath)

	// The netboot initramfs carries the matching block/filesystem modules, but
	// init_vm.sh loads only networking because storage is test-specific. Mounting
	// /dev/vda as vfat proves subsequent assertions run against Kobo-like storage
	// rather than the Linux initramfs.
	vm.run(
		"modprobe -a virtio_blk fat vfat nls_cp437 nls_iso8859_1; " +
			"mdev -s; mkdir -p /mnt/onboard; mount -t vfat /dev/vda /mnt/onboard; " +
			"mount | grep -q ' on /mnt/onboard type vfat '",
	)

	// eudev is required to execute the real installed rule; sqlite provides the
	// smallest practical way to model Nickel state from serial commands. Installing
	// them at runtime avoids a maintained root image or checked-in ARM binaries.
	vm.run("apk add --initdb eudev sqlite")

	// Extract the exact release archive at filesystem root, as Kobo's updater
	// does. Only three Nickel tables are needed by Kobodeck's read-only queries;
	// reproducing the full proprietary database would add unrelated schema churn.
	// A regular status file is enough to observe the USB messages Kobodeck sends
	// to Nickel without running Nickel itself.
	vm.run(
		"tar -xzf /test/KoboRoot.tgz -C /; " +
			"mkdir -p /mnt/onboard/.adds/kobodeck /mnt/onboard/.kobo; " +
			"cp /test/kobodeck.toml /mnt/onboard/.adds/kobodeck/kobodeck.toml; " +
			"sqlite3 /mnt/onboard/.kobo/KoboReader.sqlite " +
			"\"CREATE TABLE content (ContentID TEXT, ContentType INTEGER, ReadStatus INTEGER); " +
			"CREATE TABLE Shelf (InternalName TEXT, Name TEXT, _IsDeleted TEXT); " +
			"CREATE TABLE ShelfContent (ShelfName TEXT, ContentId TEXT, _IsDeleted TEXT);\"; " +
			"touch /tmp/nickel-hardware-status",
	)

	// QEMU names its NIC eth0, while a physical Wi-Fi connection matches wlan*.
	// Rename it before starting eudev, then synthesize add/remove uevents through
	// sysfs. The interface retains its DHCP address and route across the rename.
	vm.run(
		"ip link set eth0 down; ip link set eth0 name wlan0; ip link set wlan0 up; " +
			"mkdir -p /run/udev; udevd --daemon; udevadm control --reload-rules",
	)

	// First connection: the installed udev rule must launch the ARM binary,
	// download and convert the article onto FAT32, and request a Nickel rescan.
	firstLog := runKobodeckSync(vm, false)
	requireCleanLog(t, firstLog)
	requireLogContains(t, firstLog, "downloading ", "converted to ", "triggering Nickel rescan")
	downloadPath := fmt.Sprintf("/mnt/onboard/kobodeck/%s.kepub.epub", bookmarkID)
	vm.run("[ -s " + downloadPath + " ]")
	nickelEvents := vm.run("cat /tmp/nickel-hardware-status")
	requireLogContains(t, nickelEvents, "usb plug add", "usb plug remove")

	// Model the two manual actions performed in Nickel between connections:
	// finishing the book sets ReadStatus=2, and adding it to a shelf inserts the
	// Shelf/ShelfContent relationship. ContentID exactly matches the file:// URI
	// format queried by nickeldb.go.
	contentID := "file://" + downloadPath
	vm.run(fmt.Sprintf(
		"sqlite3 /mnt/onboard/.kobo/KoboReader.sqlite "+
			"\"INSERT INTO content VALUES ('%s', 6, 2); "+
			"INSERT INTO Shelf VALUES ('vm-favourites', '%s', 'false'); "+
			"INSERT INTO ShelfContent VALUES ('vm-favourites', '%s', 'false');\"",
		contentID,
		testFavouriteShelf,
		contentID,
	))

	// Second connection: no download should occur because the KEPUB already
	// exists. Kobodeck should instead PATCH Readeck once for archive and once for
	// favourite state, based on the Nickel rows inserted above.
	secondLog := runKobodeckSync(vm, true)
	secondRun := logSince(t, firstLog, secondLog)
	requireCleanLog(t, secondRun)
	requireLogContains(
		t,
		secondRun,
		"marking entry "+bookmarkID+" as archived",
		"marking entry "+bookmarkID+" as favourite",
	)
	if strings.Contains(secondRun, "downloading ") {
		t.Fatalf("second connection downloaded the article again:\n%s", secondRun)
	}
	state := server.bookmarkState(t, bookmarkID)
	if !state.IsArchived || !state.IsMarked {
		t.Fatalf("unexpected Readeck state after second connection: %+v", state)
	}

	// Third connection: Readeck omits the archived bookmark from the unread feed.
	// This cycle verifies idempotency—not merely final state—by checking both the
	// new log segment and total operation counts across all three runs.
	thirdLog := runKobodeckSync(vm, true)
	thirdRun := logSince(t, secondLog, thirdLog)
	requireCleanLog(t, thirdRun)
	for _, unwanted := range []string{"downloading ", "marking entry "} {
		if strings.Contains(thirdRun, unwanted) {
			t.Fatalf("third connection unexpectedly contains %q:\n%s", unwanted, thirdRun)
		}
	}
	if strings.Count(thirdLog, "downloading ") != 1 ||
		strings.Count(thirdLog, " as archived") != 1 ||
		strings.Count(thirdLog, " as favourite") != 1 {
		t.Fatalf("unexpected operation counts after three connections:\n%s", thirdLog)
	}
	vm.run("[ -s " + downloadPath + " ]")
	state = server.bookmarkState(t, bookmarkID)
	if !state.IsArchived || !state.IsMarked {
		t.Fatalf("unexpected final Readeck state: %+v", state)
	}

	// Explicit shutdown gives failures here a clear boundary and proves the guest
	// responds to the normal control protocol. The registered cleanup remains as
	// a fallback for every earlier t.Fatal path.
	vm.shutdown()
}
