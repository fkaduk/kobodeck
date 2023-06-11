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
import "io"
import "log"
import "os"
import "os/exec"

type fbinkCommandWriter struct{}

// fbinkInterface factory, detects if fbink is available and works, if
// so return a fbinkCommandWriter object or nil otherwise.
func fbinkInitialize() (fbink *fbinkCommandWriter, err error) {
	fbink = &fbinkCommandWriter{}
	// todo: don't actually write to screen, just check if fbink is
	// executable?
	err = fbink.Run("--centered", "--row", "-5", "wallabako starting...")
	return fbink, err
}

// write the given bytes to screen (after conversion to string)
//
// # This makes another fbink run, from scratch
//
// it will always return the full length of the buffer or zero, if
// there's an error
func (w *fbinkCommandWriter) Write(p []byte) (n int, err error) {
	err = w.Run("--centered", "--row", "-4", string(p))
	if err != nil {
		return 0, err
	}
	return len(p), err
}

func (w *fbinkCommandWriter) Close() (err error) {
	return w.Run("--centered", "--row", "-5", "wallabako finished")
}

// fbinkRun calls fbink with the given parameters
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
func (w *fbinkCommandWriter) Run(args ...string) (err error) {
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

// the fbinkInteractiveWriter holds the original exec.Cmd object
// representing the long-lived fbink --interactive process and its
// stdin file descriptor
//
// it otherwise implements the io.Writer interface to be fed into the
// logging module
type fbinkInteractiveWriter struct {
	cmd   exec.Cmd
	stdin io.WriteCloser
}

// start the given fbinkInteractiveWriter and initialize the struct
// correctly
func (w *fbinkInteractiveWriter) Run(args ...string) (err error) {
	w.cmd = *exec.Command("fbink", args...)

	currentPath := os.Getenv("PATH")
	desiredPath := "/mnt/onboard/.adds/koreader:/mnt/onboard/.niluje/usbnet/bin:/usr/local/kfmon/bin"
	newPath := fmt.Sprintf("%s:%s", currentPath, desiredPath)
	w.cmd.Env = append(os.Environ(), fmt.Sprintf("PATH=%s", newPath))

	w.stdin, err = w.cmd.StdinPipe()
	if err != nil {
		log.Printf("cannot create StdinPipe to fbink: %s", err)
	}

	// output is way to verbose to be useful, really clutters
	// debugging on SSH. to see what fbink actually says when
	// debugging, actually comment those out
	//
	//w.cmd.Stdout = os.Stdout
	//w.cmd.Stderr = os.Stderr
	//
	// an alternative is to use the --syslog or --quiet flags to fbink
	return w.cmd.Start()
}

// write the provided output to screen through fbink's --interactive mode
//
// implementation of the simple io.Writer interface
func (w *fbinkInteractiveWriter) Write(p []byte) (n int, err error) {
	n, err = w.stdin.Write(p)
	return n, err
}

// Close the fbink stdin stream to terminate the process
func (w *fbinkInteractiveWriter) Close() (err error) {
	log.Printf("wallabako finished.")
	err = w.stdin.Close()
	log.Printf("waiting on fbink to stop...")
	err = w.cmd.Wait()
	log.Printf("all done")
	return err
}

// startup routing for fbink --interactive
//
// caller must make sure to call the fbinkInteractiveWriter.Close()
// function to make sure the fbink process is stopped
//
// Example:
//
// fbink = fbinkInteractiveInitialize()
// defer fbink.Close()
func fbinkInteractiveInitialize() (fbink *fbinkInteractiveWriter, err error) {
	fbink = &fbinkInteractiveWriter{}
	err = fbink.Run("--interactive")
	fbink.Write([]byte("wallabako starting..."))
	return fbink, err
}
