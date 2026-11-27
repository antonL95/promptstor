package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var ErrSymlink = errors.New("refusing to write through a symlink")

type Entry struct {
	Exists     bool
	IsLink     bool
	IsDir      bool
	HardLinked bool
	LinkTarget string
	Mode       fs.FileMode
}

func Inspect(path string) (Entry, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Entry{}, nil
	}
	if err != nil {
		return Entry{}, fmt.Errorf("inspect %s: %w", path, err)
	}
	entry := Entry{Exists: true, IsDir: info.IsDir(), Mode: info.Mode(), HardLinked: hardLinked(info)}
	if info.Mode()&fs.ModeSymlink != 0 {
		entry.IsLink = true
		if entry.LinkTarget, err = os.Readlink(path); err != nil {
			return Entry{}, fmt.Errorf("read link %s: %w", path, err)
		}
	}
	return entry, nil
}

func LinkedAncestor(root, path string) (string, bool, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", false, fmt.Errorf("relate %s to %s: %w", path, root, err)
	}
	if relative == "." {
		return "", false, nil
	}
	if !filepath.IsLocal(relative) {
		return "", false, fmt.Errorf("%s is outside %s", path, root)
	}
	current := filepath.Clean(root)
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		entry, err := Inspect(current)
		if err != nil {
			return "", false, err
		}
		if entry.IsLink {
			return current, true, nil
		}
		if !entry.Exists {
			break
		}
	}
	return "", false, nil
}

func WriteAtomic(root, path string, data []byte, mode fs.FileMode) error {
	path = filepath.Clean(path)
	if err := refuseSymlinks(root, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	existing, err := Inspect(path)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if existing.Exists {
		mode = existing.Mode
	}
	if err := os.MkdirAll(filepath.Dir(path), dirModeFor(mode)); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := replaceFile(path, data, mode.Perm()); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func Remove(root, path string) error {
	path = filepath.Clean(path)
	if err := refuseSymlinks(root, path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func refuseSymlinks(root, path string) error {
	if path == filepath.Clean(root) {
		return fmt.Errorf("%s is the root itself", path)
	}
	link, linked, err := LinkedAncestor(root, path)
	if err != nil {
		return err
	}
	if linked {
		return fmt.Errorf("%w: %s", ErrSymlink, link)
	}
	return nil
}

func dirModeFor(fileMode fs.FileMode) fs.FileMode {
	if fileMode.Perm()&0o077 == 0 {
		return 0o700
	}
	return 0o755
}

func replaceFile(path string, data []byte, mode fs.FileMode) (err error) {
	temp, err := os.CreateTemp(filepath.Dir(path), ".promptstor-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, discard(temp))
		}
	}()
	if err := temp.Chmod(mode); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func discard(temp *os.File) error {
	if err := temp.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return errors.Join(err, os.Remove(temp.Name()))
	}
	return os.Remove(temp.Name())
}
