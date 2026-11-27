package schedule

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"

	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	WindowsDeferred = "Scheduled checks on Windows arrive in release 2."
	notInstalled    = "Scheduled checks are not installed."

	definitionMode fs.FileMode = 0o644
	stateMode      fs.FileMode = 0o600
)

type Runner func(name string, args ...string) ([]byte, error)

type Scheduler struct {
	Env    paths.Env
	Binary string
	Run    Runner
	Getuid func() int
}

type platform interface {
	install() (string, error)
	remove() error
	status() (string, error)
}

func (s Scheduler) Install() (string, error) {
	if s.Env.OS == "windows" {
		return WindowsDeferred, nil
	}
	p, err := s.platform()
	if err != nil {
		return "", err
	}
	if err := s.checkBinary(); err != nil {
		return "", err
	}
	return p.install()
}

func (s Scheduler) Remove() error {
	if s.Env.OS == "windows" {
		return nil
	}
	p, err := s.platform()
	if err != nil {
		return err
	}
	return p.remove()
}

func (s Scheduler) Status() (string, error) {
	if s.Env.OS == "windows" {
		return WindowsDeferred, nil
	}
	p, err := s.platform()
	if err != nil {
		return "", err
	}
	return p.status()
}

func (s Scheduler) platform() (platform, error) {
	switch s.Env.OS {
	case "darwin":
		return launchd{s}, nil
	case "linux":
		return systemd{s}, nil
	default:
		return nil, fmt.Errorf("scheduled checks are not supported on %s", s.Env.OS)
	}
}

func (s Scheduler) checkBinary() error {
	if !filepath.IsAbs(s.Binary) || strings.ContainsFunc(s.Binary, unicode.IsControl) {
		return fmt.Errorf("cannot schedule checks: the path of the promptstor binary %q is unknown or unusable", s.Binary)
	}
	return nil
}

func (s Scheduler) run(name string, args ...string) ([]byte, error) {
	out, err := s.Run(name, args...)
	if err != nil {
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

func (s Scheduler) prepareLog() error {
	path := s.Env.CheckLogPath()
	entry, err := fsx.Inspect(path)
	if err != nil {
		return err
	}
	if entry.IsLink {
		return fmt.Errorf("%w: %s", fsx.ErrSymlink, path)
	}
	if entry.Exists {
		return nil
	}
	return fsx.WriteAtomic(s.Env.StateDir(), path, nil, stateMode)
}

func (s Scheduler) writeDefinition(path string, tmpl *template.Template, data any) error {
	var content bytes.Buffer
	if err := tmpl.Execute(&content, data); err != nil {
		return fmt.Errorf("render %s: %w", path, err)
	}
	return fsx.WriteAtomic(s.Env.Home, path, content.Bytes(), definitionMode)
}

func (s Scheduler) removeDefinition(path string) error {
	err := fsx.Remove(s.Env.Home, path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func exists(path string) (bool, error) {
	entry, err := fsx.Inspect(path)
	return entry.Exists, err
}
