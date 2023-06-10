package main

import (
	"log"
	"os"
	"os/exec"
)

func ConfigureLogToExternalCommand(command string) {
	// Create a new pipe to capture log output
	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		log.Fatal(err)
	}

	// Set the log output to the pipe writer
	log.SetOutput(pipeWriter)

	// Start the external command
	cmd := exec.Command(command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = pipeReader

	err = cmd.Start()
	if err != nil {
		log.Fatal(err)
	}

	// Close the pipe writer after the command has started
	defer pipeWriter.Close()

	// Capture any log messages and write them to the pipe
	go func() {
		scanner := log.NewScanner(pipeReader)
		for scanner.Scan() {
			logLine := scanner.Text()
			// Modify or process the log line before sending it to the external command, if needed
			// ...

			// Send the log line to the external command
			_, err := pipeWriter.Write([]byte(logLine + "\n"))
			if err != nil {
				log.Println(err)
			}
		}
	}()

	// Wait for the command to finish
	err = cmd.Wait()
	if err != nil {
		log.Println(err)
	}
}

func main() {
	ConfigureLogToExternalCommand("myexternalcommand")
	// Use log.Println or other log functions to send messages to the external command
	log.Println("Hello, external command!")
}
