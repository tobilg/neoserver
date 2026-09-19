package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tobilg/neoserver/testing/protocol"
)

func main() {
	baseURL := flag.String("base-url", "http://localhost:19000", "neoserver fixture base URL")
	resultDir := flag.String("results", "test-results/protocol-integration", "result directory")
	flag.Parse()
	if err := os.MkdirAll(*resultDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	failed := make([]string, 0)
	for _, execution := range protocol.Executions() {
		fmt.Printf("Running protocol integration: %s\n", execution.ID)
		arguments := append([]string{"test", "-v", "-count=1"}, execution.Packages...)
		command := exec.Command("go", arguments...)
		command.Env = os.Environ()
		for name, value := range execution.Environment {
			if strings.HasPrefix(value, "/") {
				value = strings.TrimRight(*baseURL, "/") + value
			}
			command.Env = append(command.Env, name+"="+value)
		}
		logPath := filepath.Join(*resultDir, execution.ID+".log")
		log, err := os.Create(logPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = append(failed, execution.ID)
			continue
		}
		command.Stdout = io.MultiWriter(os.Stdout, log)
		command.Stderr = io.MultiWriter(os.Stderr, log)
		err = command.Run()
		if closeErr := log.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			failed = append(failed, execution.ID)
			fmt.Fprintf(os.Stderr, "protocol integration %s failed: %v\n", execution.ID, err)
		}
	}
	if len(failed) > 0 {
		fmt.Fprintf(os.Stderr, "failed protocol integrations: %s\n", strings.Join(failed, ", "))
		os.Exit(1)
	}
	fmt.Println("all protocol integrations passed")
}
