package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	_ "github.com/antonL95/promptstor/internal/adapter/all"
	"github.com/antonL95/promptstor/internal/cli"
	"github.com/antonL95/promptstor/internal/paths"
)

func main() {
	env, err := paths.FromOS()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	root := cli.NewRoot(cli.Deps{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Env:    env,
		Now:    time.Now,
		Run:    runCommand,
	})
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func runCommand(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}
