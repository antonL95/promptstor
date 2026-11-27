package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		RunIn:  runCommandIn,
		Binary: binaryPath(),
	})
	if err := root.Execute(); err != nil {
		var exit cli.ExitCode
		if errors.As(err, &exit) {
			os.Exit(int(exit))
		}
		os.Exit(1)
	}
}

func binaryPath() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return resolved
}

func runCommand(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

func runCommandIn(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.Output()
}
