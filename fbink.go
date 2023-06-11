package main

/*

   Rudimentary interface for the fbink binary, which allows displaying
   messages as an overlay on Kobo readers and others.

   https://github.com/NiLuJe/FBInk

   This assumes the `fbink` binary is somehow available in the PATH or
   some magic location (koreader, kfmon and niluje's usbnet package
   supported).

   We do not use the Golang library because it (probably?) depends on
   cgo, which we're allergic to because of cross-compilation here.

   See:

   https://github.com/shermp/go-fbink-v2
*/
import "fmt"
import "os"
import "os/exec"

type fbinkWriter struct{}

func (w *fbinkWriter) Write(p []byte) (n int, err error) {
	cmd := exec.Command("fbink", "--centered", "--row", "-5", "--overlay", string(p))

	currentPath := os.Getenv("PATH")
	desiredPath := "/mnt/onboard/.adds/koreader:/mnt/onboard/.niluje/usbnet/bin:/usr/local/kfmon/bin"
	newPath := fmt.Sprintf("%s:%s", currentPath, desiredPath)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PATH=%s", newPath))

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err = cmd.Run(); err != nil {
		log.Printf("fbink start failed: %s", err)
		return 0, err
	}
	return len(p), nil
}

func (w *fbinkWriter) Close() error {
	return nil
}
