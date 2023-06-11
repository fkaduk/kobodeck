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

// fbinkInterface factory, detects if fbink is availble and works, if
// so return a fbinkWriter object or nil otherwise.
func fbinkInitialize() (fbink *fbinkWriter, err error) {
	fbink = &fbinkWriter{}
	// todo: don't actually write to screen, just check if fbink is
	// executable?
	_, err = fbink.Write([]byte("wallabako starting"))
	return fbink, err
}

func (w *fbinkWriter) Write(p []byte) (n int, err error) {
	err = w.Run("--centered", "--row", "-5", "--overlay", string(p))
	if err != nil {
		return 0, err
	}
	return len(p), err
}

func (w *fbinkWriter) Close() error {
	return nil
}

// (w *fbinkWriter) fbinkRun calls fbink with the given parameters
//
// this is a separate function because of how clunky calling fbink actually is
func (w *fbinkWriter) Run(args ...string) (err error) {
	cmd := exec.Command("fbink", args...)

	currentPath := os.Getenv("PATH")
	desiredPath := "/mnt/onboard/.adds/koreader:/mnt/onboard/.niluje/usbnet/bin:/usr/local/kfmon/bin"
	newPath := fmt.Sprintf("%s:%s", currentPath, desiredPath)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PATH=%s", newPath))

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err = cmd.Run(); err != nil {
		return err
	}
	return nil
}
