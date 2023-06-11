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
	err = fbink.Run("--centered", "--row", "-5", "wallabako starting...")
	return fbink, err
}

func (w *fbinkWriter) Write(p []byte) (n int, err error) {
	err = w.Run("--centered", "--row", "-4", string(p))
	if err != nil {
		return 0, err
	}
	return len(p), err
}

func (w *fbinkWriter) Close() (err error) {
	return w.Run("--centered", "--row", "-5", "wallabako finished")
}

// (w *fbinkWriter) fbinkRun calls fbink with the given parameters
//
// this is a separate function because of how clunky calling fbink
// actually is.
//
// There's actually an "--interactive" flag that outputs all lines fed
// on stdin one by one. Problem is it quickly overflows
// downwards.
//
// It's also hellish to implement an io.Writer that pipes into a
// command. I *think* I've managed to do it - but could never quite
// confirm it, as I was working with cat(1) which has different
// behavior than fbink.
//
// The basic idea would be to have the fbink command working in the
// background and having a Writer interface on top of it that would
// write to the cmd.StdinPipe().
//
// See 7974548 (implement basic fbink output (#49), 2023-06-09) for
// when the prototype working with cat(1) was ripped out.
func (w *fbinkWriter) Run(args ...string) (err error) {
	cmd := exec.Command("fbink", args...)

	currentPath := os.Getenv("PATH")
	desiredPath := "/mnt/onboard/.adds/koreader:/mnt/onboard/.niluje/usbnet/bin:/usr/local/kfmon/bin"
	newPath := fmt.Sprintf("%s:%s", currentPath, desiredPath)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PATH=%s", newPath))

	// output is way to verbose to be useful, really clutters
	// debugging on SSH. to see what fbink actually says when
	// debugging, actually comment those out
	//
	//cmd.Stdout = os.Stdout
	//cmd.Stderr = os.Stderr
	return cmd.Run()
}
