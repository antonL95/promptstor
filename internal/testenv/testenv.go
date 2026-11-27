package testenv

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/antonL95/promptstor/internal/paths"
)

func Home(t testing.TB, fixtureDir string) paths.Env {
	t.Helper()
	home := t.TempDir()
	if err := copyTree(fixtureDir, home); err != nil {
		t.Fatal(err)
	}
	return paths.Env{
		Home:       home,
		ConfigHome: filepath.Join(home, ".config"),
		StateHome:  filepath.Join(home, ".local", "state"),
		OS:         "darwin",
		Getenv:     func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
		},
	}
}

type copiedDir struct {
	path string
	mode fs.FileMode
}

func copyTree(source, destination string) error {
	var dirs []copiedDir
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			if relative == "." {
				return nil
			}
			dirs = append(dirs, copiedDir{target, info.Mode().Perm()})
			return os.Mkdir(target, 0o700)
		case entry.Type()&fs.ModeSymlink != 0:
			return copyLink(path, target, relative)
		default:
			return copyFile(path, target, info.Mode().Perm())
		}
	})
	if err != nil {
		return fmt.Errorf("copy fixture %s: %w", source, err)
	}
	for _, dir := range slices.Backward(dirs) {
		if err := os.Chmod(dir.path, dir.mode); err != nil {
			return fmt.Errorf("copy fixture %s: %w", source, err)
		}
	}
	return nil
}

func copyLink(path, target, relative string) error {
	linked, err := os.Readlink(path)
	if err != nil {
		return err
	}
	if filepath.IsAbs(linked) || !filepath.IsLocal(filepath.Join(filepath.Dir(relative), linked)) {
		return fmt.Errorf("link %s points outside the fixture", relative)
	}
	return os.Symlink(linked, target)
}

func copyFile(path, target string, mode fs.FileMode) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(target, content, mode); err != nil {
		return err
	}
	return os.Chmod(target, mode)
}
