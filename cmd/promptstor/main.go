package main

import (
	"os"

	"github.com/antonL95/promptstor/internal/cli"
)

func main() {
	root := cli.NewRoot(cli.Deps{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
