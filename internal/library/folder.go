package library

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/fsx"
)

type folder struct {
	files, junk, dirs []string
}

func isHidden(name string) bool { return strings.HasPrefix(name, ".") }

func isJunk(name string) bool {
	return name == ".DS_Store" || (strings.HasPrefix(name, ".promptstor-") && strings.HasSuffix(name, ".tmp"))
}

func scanFolder(root string) (folder, error) {
	var found folder
	entry, err := fsx.Inspect(root)
	if err != nil || !entry.Exists {
		return found, err
	}
	if !entry.IsDir && !entry.IsLink {
		return found, fmt.Errorf("%s is not a folder", root)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case path != root && isHidden(entry.Name()):
			if entry.Type().IsRegular() && isJunk(entry.Name()) {
				found.junk = append(found.junk, path)
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
		case entry.Type()&fs.ModeSymlink != 0:
			return symlinkError(path)
		case entry.IsDir():
			found.dirs = append(found.dirs, path)
		case !entry.Type().IsRegular():
			return fmt.Errorf("%s is not a regular file", path)
		default:
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			found.files = append(found.files, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return folder{}, fmt.Errorf("scan %s: %w", root, err)
	}
	return found, nil
}

func symlinkError(path string) error {
	return fmt.Errorf("%s is a symlink, which is not followed", path)
}

func readFile(path string) (File, error) {
	entry, err := fsx.Inspect(path)
	if err != nil {
		return File{}, err
	}
	if entry.IsLink {
		return File{}, symlinkError(path)
	}
	if !entry.Mode.IsRegular() {
		return File{}, fmt.Errorf("%s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read %s: %w", path, err)
	}
	return File{Data: data, Mode: entry.Mode.Perm()}, nil
}

func pruneEmptyDirs(layerDir string, dirs []string) error {
	for _, dir := range slices.Backward(dirs) {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", dir, err)
		}
		if len(entries) == 0 {
			if err := fsx.Remove(layerDir, dir); err != nil {
				return err
			}
		}
	}
	return nil
}
