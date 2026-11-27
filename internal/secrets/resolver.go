package secrets

import (
	"errors"
	"fmt"
	"strings"

	"github.com/antonL95/promptstor/internal/paths"
)

type Resolver interface {
	Resolve(ref string) (string, error)
}

type osResolver struct {
	env paths.Env
	run func(name string, args ...string) ([]byte, error)
}

func OSResolver(env paths.Env, run func(name string, args ...string) ([]byte, error)) Resolver {
	return osResolver{env: env, run: run}
}

func (r osResolver) Resolve(ref string) (string, error) {
	if name, found := strings.CutPrefix(ref, envScheme); found {
		return r.variable(name)
	}
	if entry, found := strings.CutPrefix(ref, keychainScheme); found {
		return r.keychain(entry)
	}
	return "", errors.New("unknown reference scheme")
}

func (r osResolver) variable(name string) (string, error) {
	if r.env.Getenv != nil {
		if value := r.env.Getenv(name); value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("environment variable %s is unset or empty", name)
}

func (r osResolver) keychain(entry string) (string, error) {
	split := strings.LastIndex(entry, "/")
	if split <= 0 || split == len(entry)-1 {
		return "", errors.New("a keychain reference is keychain:<service>/<account>")
	}
	service, account := entry[:split], entry[split+1:]
	var command string
	var args []string
	switch r.env.OS {
	case "darwin":
		command, args = "security", []string{"find-generic-password", "-s", service, "-a", account, "-w"}
	case "linux":
		command, args = "secret-tool", []string{"lookup", "service", service, "account", account}
	default:
		return "", fmt.Errorf("keychain references are not supported on %q", r.env.OS)
	}
	output, err := r.run(command, args...)
	if err != nil {
		return "", fmt.Errorf("run %s: %w", command, err)
	}
	return strings.TrimSuffix(string(output), "\n"), nil
}
