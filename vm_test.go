//go:build vmtest

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
	vmSSHTimeout      = 90 * time.Second
	qemuHostGatewayIP = "10.0.2.2"
)

type koboVM struct {
	t          *testing.T
	sshKey     string
	sshPort    int
	serialPath string
	cmd        *exec.Cmd
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("required command %q was not found: %v", name, err)
	}
}

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

func reserveTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func startKoboVM(t *testing.T, imageDir, sshKey string) *koboVM {
	t.Helper()
	port := reserveTCPPort(t)
	serialPath := filepath.Join(t.TempDir(), "serial.log")
	serial, err := os.Create(serialPath)
	if err != nil {
		t.Fatal(err)
	}

	args := []string{
		"-machine", "virt",
		"-cpu", "cortex-a15",
		"-smp", "1",
		"-m", "512",
		"-nographic",
		"-no-reboot",
		"-kernel", filepath.Join(imageDir, "vmlinuz-virt"),
		"-initrd", filepath.Join(imageDir, "initramfs-virt"),
		"-append", "root=/dev/vda rootfstype=ext4 rw rootwait console=ttyAMA0 init=/sbin/kobodeck-test-init",
		// QEMU's ARM virt machine enumerates virtio-mmio block devices in
		// reverse creation order. Attach onboard first so root becomes vda.
		"-drive", "file=" + filepath.Join(imageDir, "onboard.fat") + ",if=none,format=raw,id=onboard",
		"-device", "virtio-blk-device,drive=onboard",
		"-drive", "file=" + filepath.Join(imageDir, "root.ext4") + ",if=none,format=raw,id=root",
		"-device", "virtio-blk-device,drive=root",
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

func (vm *koboVM) sshRun(command string) (string, error) {
	args := append(vm.sshArgs(), "root@127.0.0.1", command)
	output, err := exec.Command("ssh", args...).CombinedOutput()
	return string(output), err
}

func (vm *koboVM) mustRun(command string) string {
	vm.t.Helper()
	output, err := vm.sshRun(command)
	if err != nil {
		vm.t.Fatalf("guest command %q: %v\n%s", command, err, output)
	}
	return output
}

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

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func TestKoboVMReadeckAPISmoke(t *testing.T) {
	for _, command := range []string{"docker", "mkfs.ext4", "mkfs.vfat", "qemu-system-arm", "ssh", "ssh-keygen"} {
		requireCommand(t, command)
	}

	// TestMain owns the isolated Readeck container on the host. Only its mapped
	// HTTP endpoint is exposed to this Docker-free ARM guest through QEMU slirp.
	bookmarkID := createLoadedBookmark(t, testBookmarkURL)

	workDir := t.TempDir()
	sshKey := filepath.Join(workDir, "id_ed25519")
	runHostCommand(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", sshKey)
	imageDir := filepath.Join(workDir, "image")
	runHostCommand(t, "testvm/build-image.sh", imageDir, sshKey+".pub")

	vm := startKoboVM(t, imageDir, sshKey)
	apiURL := readeckURLFromVM(t) + "/api/bookmarks/" + bookmarkID
	output := vm.mustRun(
		"wget -qO- --header=" +
			shellQuote("Authorization: Bearer "+config.Server.Token) + " " +
			shellQuote(apiURL),
	)

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
