//go:build vmtest

package main

import (
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

const vmSSHTimeout = 90 * time.Second

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
		"-append", "root=/dev/vda rw rootwait console=ttyAMA0 init=/sbin/kobodeck-test-init",
		"-drive", "file=" + filepath.Join(imageDir, "root.ext4") + ",if=none,format=raw,id=root",
		"-device", "virtio-blk-device,drive=root",
		"-drive", "file=" + filepath.Join(imageDir, "onboard.fat") + ",if=none,format=raw,id=onboard",
		"-device", "virtio-blk-device,drive=onboard",
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

func (vm *koboVM) copyTo(localPath, remotePath string) {
	vm.t.Helper()
	args := []string{
		"-F", "/dev/null",
		"-i", vm.sshKey,
		"-P", strconv.Itoa(vm.sshPort),
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		localPath,
		"root@127.0.0.1:" + remotePath,
	}
	if output, err := exec.Command("scp", args...).CombinedOutput(); err != nil {
		vm.t.Fatalf("copy %s to guest: %v\n%s", localPath, err, output)
	}
}

func (vm *koboVM) logCount(marker string) int {
	vm.t.Helper()
	output := strings.TrimSpace(vm.mustRun("grep -c '" + marker + "' /mnt/onboard/.adds/kobodeck/kobodeck.log 2>/dev/null || true"))
	if output == "" {
		return 0
	}
	count, err := strconv.Atoi(output)
	if err != nil {
		vm.t.Fatalf("parse run count %q: %v", output, err)
	}
	return count
}

func (vm *koboVM) runCount() int {
	return vm.logCount("loaded configuration")
}

func (vm *koboVM) waitForCompletedRunCount(want int) {
	vm.t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if vm.logCount("completed in") >= want {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	logOutput := vm.mustRun("cat /mnt/onboard/.adds/kobodeck/kobodeck.log 2>/dev/null || true")
	vm.t.Fatalf("timed out waiting for %d Kobodeck runs; log:\n%s", want, logOutput)
}

func writeVMConfig(t *testing.T, path string) {
	t.Helper()
	serverURL, err := url.Parse(config.Server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(serverURL.Host)
	if err != nil {
		t.Fatalf("parse Readeck address %q: %v", config.Server.URL, err)
	}
	content := fmt.Sprintf(`[Server]
URL = %s
Token = %s
Timeout = 20

[Fetch]
Workers = 1
Limit = 0
Labels = ""
Status = "unread"

[Sync]
Archive = false
FavouriteCollection = ""

[Log]
Verbose = true
Size = 1

[Output]
Path = "/mnt/onboard/kobodeck"
Delete = false
`, strconv.Quote("http://10.0.2.2:"+port), strconv.Quote(config.Server.Token))
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestKoboVMNetworkTriggers(t *testing.T) {
	for _, command := range []string{"docker", "mkfs.ext4", "mkfs.vfat", "qemu-system-arm", "scp", "ssh", "ssh-keygen"} {
		requireCommand(t, command)
	}

	bookmarkID := createLoadedBookmark(t, testBookmarkURL)
	runHostCommand(t, "make", "tarball")

	workDir := t.TempDir()
	sshKey := filepath.Join(workDir, "id_ed25519")
	runHostCommand(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", sshKey)
	imageDir := filepath.Join(workDir, "image")
	runHostCommand(t, "testvm/build-image.sh", imageDir, sshKey+".pub")

	vm := startKoboVM(t, imageDir, sshKey)
	configPath := filepath.Join(workDir, "kobodeck.toml")
	dbPath := filepath.Join(workDir, "KoboReader.sqlite")
	writeVMConfig(t, configPath)
	createDB(t, dbPath, nickelSchema)

	vm.copyTo("build/KoboRoot.tgz", "/tmp/KoboRoot.tgz")
	vm.mustRun("tar -xzf /tmp/KoboRoot.tgz -C / && udevadm control --reload-rules")
	vm.mustRun("mkdir -p /mnt/onboard/.adds/kobodeck /mnt/onboard/.kobo && touch /tmp/nickel-hardware-status")
	vm.copyTo(configPath, "/mnt/onboard/.adds/kobodeck/kobodeck.toml")
	vm.copyTo(dbPath, "/mnt/onboard/.kobo/KoboReader.sqlite")

	if got := vm.runCount(); got != 0 {
		t.Fatalf("Kobodeck ran before Wi-Fi was enabled: got %d runs", got)
	}

	vm.mustRun("modprobe dummy && ip link add wlan0 type dummy")
	vm.waitForCompletedRunCount(1)
	vm.mustRun("test -s /mnt/onboard/kobodeck/" + bookmarkID + ".kepub.epub")
	status := vm.mustRun("cat /tmp/nickel-hardware-status")
	if !strings.Contains(status, "usb plug add\n") || !strings.Contains(status, "usb plug remove\n") {
		t.Fatalf("Nickel rescan events missing:\n%s", status)
	}

	vm.mustRun("ip link delete wlan0 && sleep 2")
	if got := vm.runCount(); got != 1 {
		t.Fatalf("removing Wi-Fi launched Kobodeck: got %d runs", got)
	}

	vm.mustRun("ip link add wlan0 type dummy")
	vm.waitForCompletedRunCount(2)
	vm.mustRun("ip link delete wlan0 && ip link add eth9 type dummy")
	vm.waitForCompletedRunCount(3)

	logOutput := vm.mustRun("cat /mnt/onboard/.adds/kobodeck/kobodeck.log")
	if !strings.Contains(logOutput, "connecting to http://10.0.2.2:") {
		t.Errorf("VM log does not show the test Readeck connection:\n%s", logOutput)
	}
	if got := strings.Count(logOutput, "loaded configuration"); got != 3 {
		t.Errorf("expected exactly 3 udev-triggered runs, got %d:\n%s", got, logOutput)
	}
}
