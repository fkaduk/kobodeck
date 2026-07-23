//go:build vmtest

// This file contains the host-side controller for the ARM system-VM smoke test.
//
// The topology deliberately mirrors a real installation:
//
//   - Readeck runs outside the simulated Kobo. TestMain starts it in a normal
//     host-side Docker container and publishes its HTTP port on the host.
//   - QEMU runs a complete 32-bit ARM Linux guest that represents the Kobo.
//   - QEMU's user-mode network exposes the host to the guest as 10.0.2.2.
//   - SSH is only a control channel used by the Go test to execute the API call
//     and inspect its result.
//
// Keeping this behind the vmtest build tag avoids requiring QEMU and the image
// construction tools during ordinary `go test` runs.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// The guest normally reaches sshd in roughly 30 seconds under emulation.
	// Leave ample headroom for slower CI runners while still failing a wedged
	// boot much sooner than the overall `go test` timeout.
	vmSSHTimeout = 90 * time.Second

	// QEMU slirp reserves 10.0.2.2 as the host-side gateway visible to a
	// guest. A service bound to a host loopback port can therefore be reached
	// from the VM by replacing 127.0.0.1 with this address.
	qemuHostGatewayIP = "10.0.2.2"
)

// koboVM owns a running QEMU process and the information required to reach its
// SSH server through QEMU's host port forwarding.
//
// All fields are host-side values. In particular, sshPort is the ephemeral
// localhost port forwarded to port 22 in the guest.
type koboVM struct {
	t          *testing.T
	sshKey     string
	sshPort    int
	serialPath string
	cmd        *exec.Cmd
}

// requireCommand gives a direct, actionable error before the test performs any
// expensive container or VM setup.
func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("required command %q was not found: %v", name, err)
	}
}

// runHostCommand executes one of the build/control programs on the host. Its
// combined output is included on failure because Docker and image-building
// errors are otherwise difficult to diagnose from the VM serial console.
func runHostCommand(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// reserveTCPPort asks the host kernel for a currently unused loopback port.
// QEMU uses this port to forward host TCP connections to guest port 22.
//
// The listener must be closed before QEMU starts so QEMU can bind the port.
// There is consequently a small theoretical bind race, but using a
// kernel-selected ephemeral port keeps collisions very unlikely in practice.
func reserveTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// startKoboVM boots the disk images produced by testvm/build-image.sh and waits
// until the guest is controllable over SSH.
//
// Serial output is always captured to a file instead of being mixed into normal
// test output. It is printed automatically if the test fails, which gives us
// kernel, initramfs, udev, DHCP, and sshd diagnostics in one place.
func startKoboVM(t *testing.T, imageDir, sshKey string) *koboVM {
	t.Helper()
	port := reserveTCPPort(t)
	serialPath := filepath.Join(t.TempDir(), "serial.log")
	serial, err := os.Create(serialPath)
	if err != nil {
		t.Fatal(err)
	}

	args := []string{
		// "virt" is QEMU's generic ARM platform. It provides standard virtio
		// block and network devices without pretending to emulate a specific
		// Kobo motherboard.
		"-machine", "virt",

		// cortex-a15 is a 32-bit ARMv7 CPU. One CPU and 512 MiB keep the guest
		// lightweight while leaving enough memory for Alpine, eudev, and sshd.
		"-cpu", "cortex-a15",
		"-smp", "1",
		"-m", "512",

		// Run headlessly and route the PL011 serial console to QEMU stdout.
		// -no-reboot preserves the useful final state if init crashes.
		"-nographic",
		"-no-reboot",

		// Boot Alpine's kernel/initramfs directly. rootfstype is explicit so
		// the initramfs loads the modular ext4 driver before mounting vda.
		// The custom init starts only the services needed by this test.
		"-kernel", filepath.Join(imageDir, "vmlinuz-virt"),
		"-initrd", filepath.Join(imageDir, "initramfs-virt"),
		"-append", "root=/dev/vda rootfstype=ext4 rw rootwait console=ttyAMA0 init=/sbin/kobodeck-test-init",

		// QEMU's ARM virt machine enumerates virtio-mmio block devices in
		// reverse creation order. Attach the FAT onboard image first so the
		// ext4 root image becomes /dev/vda and onboard becomes /dev/vdb.
		"-drive", "file=" + filepath.Join(imageDir, "onboard.fat") + ",if=none,format=raw,id=onboard",
		"-device", "virtio-blk-device,drive=onboard",
		"-drive", "file=" + filepath.Join(imageDir, "root.ext4") + ",if=none,format=raw,id=root",
		"-device", "virtio-blk-device,drive=root",

		// QEMU user networking avoids bridge/tap setup and root privileges.
		// The guest receives an address such as 10.0.2.15 and sees the host at
		// 10.0.2.2. Only SSH is forwarded back to a host loopback port.
		"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp:127.0.0.1:%d-:22", port),
		"-device", "virtio-net-device,netdev=net0",
	}
	cmd := exec.Command("qemu-system-arm", args...)
	cmd.Stdout = serial
	cmd.Stderr = serial
	if err := cmd.Start(); err != nil {
		serial.Close()
		t.Fatal(err)
	}

	vm := &koboVM{t: t, sshKey: sshKey, sshPort: port, serialPath: serialPath, cmd: cmd}

	// Register cleanup immediately after QEMU starts. Ask QEMU to exit
	// gracefully first, then kill it after a short deadline so a broken guest
	// cannot hang the entire test process. Wait is called exactly once.
	t.Cleanup(func() {
		if vm.cmd.Process != nil {
			_ = vm.cmd.Process.Signal(os.Interrupt)
			done := make(chan struct{})
			go func() {
				_ = vm.cmd.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = vm.cmd.Process.Kill()
				<-done
			}
		}
		_ = serial.Close()
		if t.Failed() {
			if output, err := os.ReadFile(serialPath); err == nil {
				t.Logf("QEMU serial console:\n%s", output)
			}
		}
	})

	// An open forwarded TCP port is not enough to prove that boot completed:
	// sshd must accept an authenticated command. Polling `true` verifies the
	// kernel, root filesystem, custom init, DHCP, port forward, and SSH key.
	deadline := time.Now().Add(vmSSHTimeout)
	for time.Now().Before(deadline) {
		if out, err := vm.sshRun("true"); err == nil {
			return vm
		} else if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			t.Fatalf("QEMU exited before SSH was ready: %s", out)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("VM did not become ready over SSH within %s", vmSSHTimeout)
	return nil
}

// sshArgs returns deliberately isolated SSH client settings:
//
//   - no user or machine-wide SSH configuration is loaded;
//   - password prompts are forbidden;
//   - the disposable test host is not written to known_hosts;
//   - routine first-connection warnings are suppressed so sshRun can return
//     clean command output, including JSON.
func (vm *koboVM) sshArgs() []string {
	return []string{
		"-F", "/dev/null",
		"-i", vm.sshKey,
		"-p", strconv.Itoa(vm.sshPort),
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=2",
		"-o", "LogLevel=ERROR",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
	}
}

// sshRun executes a shell command as root in the guest and returns both stdout
// and stderr. Combining them makes command failures self-contained; LogLevel
// is kept at ERROR above so successful JSON output remains parseable.
func (vm *koboVM) sshRun(command string) (string, error) {
	args := append(vm.sshArgs(), "root@127.0.0.1", command)
	output, err := exec.Command("ssh", args...).CombinedOutput()
	return string(output), err
}

// mustRun is the assertion-oriented wrapper used once the VM is known to be
// ready. Marking it as a helper points failures at the test call site.
func (vm *koboVM) mustRun(command string) string {
	vm.t.Helper()
	output, err := vm.sshRun(command)
	if err != nil {
		vm.t.Fatalf("guest command %q: %v\n%s", command, err, output)
	}
	return output
}

// readeckURLFromVM translates TestMain's host-side Readeck URL into the URL
// visible inside QEMU.
//
// Testcontainers publishes Readeck on a random localhost port. The port stays
// the same across the boundary; only the hostname changes from the host's
// loopback address to QEMU's 10.0.2.2 gateway.
func readeckURLFromVM(t *testing.T) string {
	t.Helper()
	serverURL, err := url.Parse(config.Server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(serverURL.Host)
	if err != nil {
		t.Fatalf("parse Readeck address %q: %v", config.Server.URL, err)
	}
	return "http://" + net.JoinHostPort(qemuHostGatewayIP, port)
}

// shellQuote safely turns arbitrary data into one POSIX-shell argument. This is
// important for the bearer token: it must be interpreted as header data, never
// as guest shell syntax.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// TestKoboVMReadeckAPISmoke verifies only the lowest useful integration layer:
//
//  1. the host-side Readeck container is healthy and authenticated;
//  2. the ARM guest boots and receives QEMU networking;
//  3. a raw HTTP client inside the guest can reach Readeck;
//  4. Readeck accepts the bearer token and returns the expected bookmark.
//
// It intentionally does not build/install Kobodeck or exercise udev yet. That
// keeps failures in this test attributable to VM boot or network topology.
func TestKoboVMReadeckAPISmoke(t *testing.T) {
	// Docker builds the Alpine rootfs on the host; it is not installed in the
	// guest. The mkfs tools construct the two disk images consumed by QEMU.
	for _, command := range []string{"docker", "mkfs.ext4", "mkfs.vfat", "qemu-system-arm", "ssh", "ssh-keygen"} {
		requireCommand(t, command)
	}

	// TestMain owns the isolated Readeck container on the host. Only its mapped
	// HTTP endpoint is exposed to this Docker-free ARM guest through QEMU slirp.
	bookmarkID := createLoadedBookmark(t, testBookmarkURL)

	// Everything generated for this VM is test-local and removed by Go's test
	// cleanup. The private key never enters the repository or Docker image.
	workDir := t.TempDir()
	sshKey := filepath.Join(workDir, "id_ed25519")
	runHostCommand(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", sshKey)
	imageDir := filepath.Join(workDir, "image")
	runHostCommand(t, "testvm/build-image.sh", imageDir, sshKey+".pub")

	vm := startKoboVM(t, imageDir, sshKey)

	// BusyBox wget is part of Alpine and provides the most direct possible
	// smoke check: no Kobodeck code participates in this request.
	apiURL := readeckURLFromVM(t) + "/api/bookmarks/" + bookmarkID
	output := vm.mustRun(
		"wget -qO- --header=" +
			shellQuote("Authorization: Bearer "+config.Server.Token) + " " +
			shellQuote(apiURL),
	)

	// Decode only the fields needed to prove we received the requested,
	// fully-loaded bookmark rather than a proxy error or login page.
	var bookmark struct {
		URL    string `json:"url"`
		Loaded bool   `json:"loaded"`
	}
	if err := json.Unmarshal([]byte(output), &bookmark); err != nil {
		t.Fatalf("decode Readeck API response from VM: %v\n%s", err, output)
	}
	if bookmark.URL != testBookmarkURL || !bookmark.Loaded {
		t.Fatalf("unexpected Readeck API response from VM: %+v", bookmark)
	}
}
