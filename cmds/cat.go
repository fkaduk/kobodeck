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
		log.Fatal(err)
	}
	defer stdout.Close()

	err = cmd.Start()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("started cat")

	log.SetOutput(stdout)
	log.Printf("set output")
	log.Printf("test logging to cat\n")
	err = stdout.Close()
	err2 := cmd.Wait()
	log.SetOutput(os.Stdout)
	log.Printf("Command finished with error: %v", err)
	log.Printf("Command finished with error: %v", err2)
}
