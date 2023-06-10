package main

import "log"
import "os"
import "os/exec"

func main() {
	log.Printf("starting")
	cmd := exec.Command("cat")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdinPipe()
	if err != nil {
		log.Printf("stdinpipe failed")
		log.Fatal(err)
	}
	defer stdout.Close()

	err = cmd.Start()
	if err != nil {
		log.Printf("start failed")
		log.Fatal(err)
	}
	log.Printf("started cat")

	log.SetOutput(stdout)
	log.Printf("set output")
	log.Printf("test logging to cat\n")
	log.SetOutput(os.Stdout)
	log.Printf("before close")
	err = stdout.Close()
	log.Printf("before wait")
	err2 := cmd.Wait()
	log.Printf("Command finished with error: %v", err)
	log.Printf("Command finished with error: %v", err2)
}
