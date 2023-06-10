package main

import "log"
import "os/exec"

func main() {
	cmd := exec.Command("cat")
	stdout, err := cmd.StdinPipe()

	if err != nil {
		log.Fatal(err)
	}
	defer stdout.Close()

	err = cmd.Start()
	if err != nil {
		log.Fatal(err)
	}
	defer cmd.Wait()

	log.SetOutput(stdout)
	log.Printf("test logging to cat")
	stdout.Close()
}
