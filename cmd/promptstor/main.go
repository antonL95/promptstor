package main

import (
	"fmt"
	"os"
	"time"

	"github.com/antonL95/promptstor/internal/cli"
	"github.com/antonL95/promptstor/internal/paths"
)

func main() {
	env, err := paths.FromOS()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	root := cli.NewRoot(cli.Deps{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Env: env, Now: time.Now})
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
