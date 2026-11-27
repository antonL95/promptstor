package paths

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func FromOS() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, fmt.Errorf("find home directory: %w", err)
	}
	env := Env{Home: home, OS: runtime.GOOS, Getenv: os.Getenv, LookPath: exec.LookPath}.WithBaseDirs()
	if err := env.Validate(); err != nil {
		return Env{}, err
	}
	return env, nil
}
