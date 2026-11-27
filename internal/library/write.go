package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/antonL95/promptstor/internal/fsx"
)

const mainMode fs.FileMode = 0o644

func canonical(it Item) ([]byte, error) {
	data, err := encodeItem(it)
	if err != nil {
		return nil, err
	}
	reloaded, err := decodeItem(it.Kind, writeSlug(it), data)
	if err != nil {
		return nil, fmt.Errorf("the item cannot be stored exactly: %w", err)
	}
	again, err := encodeItem(reloaded)
	if err != nil || !bytes.Equal(data, again) || reloaded.Body != it.Body {
		return nil, errors.New("the item cannot be stored exactly")
	}
	return data, nil
}

func WriteItem(dir string, it Item) (string, error) {
	l, err := layoutOf(it.Kind)
	if err != nil {
		return "", fmt.Errorf("write item %q: %w", it.Name, err)
	}
	if err := validateForWrite(l, it); err != nil {
		return "", fmt.Errorf("write %s %q: %w", it.Kind, it.Name, err)
	}
	data, err := canonical(it)
	if err != nil {
		return "", fmt.Errorf("write %s %q: %w", it.Kind, it.Name, err)
	}
	slug := writeSlug(it)
	path := l.mainPath(dir, slug)
	targets := []string{path}
	for key := range it.Files {
		targets = append(targets, filepath.Join(l.folderPath(dir, slug), filepath.FromSlash(key)))
	}
	if err := refuseLinked(dir, targets); err != nil {
		return "", fmt.Errorf("write %s %q: %w", it.Kind, it.Name, err)
	}
	if err := refuseOtherItem(path, slug, it); err != nil {
		return "", fmt.Errorf("write %s %q: %w", it.Kind, it.Name, err)
	}
	if l.folder {
		if err := dropStale(dir, l, slug, it.Files); err != nil {
			return "", fmt.Errorf("write %s %q: %w", it.Kind, it.Name, err)
		}
	}
	if err := fsx.WriteAtomic(dir, path, data, mainMode); err != nil {
		return "", err
	}
	for _, key := range slices.Sorted(maps.Keys(it.Files)) {
		target := filepath.Join(l.folderPath(dir, slug), filepath.FromSlash(key))
		if err := writeAttachment(dir, target, it.Files[key]); err != nil {
			return "", err
		}
	}
	return path, nil
}

func refuseLinked(dir string, targets []string) error {
	for _, target := range targets {
		link, linked, err := fsx.LinkedAncestor(dir, target)
		if err != nil {
			return err
		}
		if linked {
			return fmt.Errorf("%w: %s", fsx.ErrSymlink, link)
		}
	}
	return nil
}

func refuseOtherItem(path, slug string, it Item) error {
	entry, err := fsx.Inspect(path)
	if err != nil || !entry.Exists {
		return err
	}
	existing, err := readFile(path)
	if err != nil {
		return err
	}
	stored, err := decodeItem(it.Kind, slug, existing.Data)
	if err != nil {
		return fmt.Errorf("%s exists and its id cannot be read: %w", path, err)
	}
	if stored.ID != it.ID {
		return fmt.Errorf("%s holds id %q, not %q", path, stored.ID, it.ID)
	}
	return nil
}

func dropStale(dir string, l layout, slug string, keep map[string]File) error {
	root := l.folderPath(dir, slug)
	found, err := scanFolder(root)
	if err != nil {
		return err
	}
	for _, key := range found.files {
		if _, kept := keep[key]; kept || (l.kind == KindSkill && key == skillFile) {
			continue
		}
		if err := fsx.Remove(dir, filepath.Join(root, filepath.FromSlash(key))); err != nil {
			return err
		}
	}
	return pruneEmptyDirs(dir, found.dirs)
}

func fileMode(file File) fs.FileMode {
	if file.Mode.Perm() == 0 {
		return mainMode
	}
	return file.Mode.Perm()
}

func writeAttachment(dir, path string, file File) error {
	mode := fileMode(file)
	if err := fsx.WriteAtomic(dir, path, file.Data, mode); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("set mode of %s: %w", path, err)
	}
	return nil
}

func DeleteItem(dir string, it Item) error {
	l, err := layoutOf(it.Kind)
	if err != nil {
		return fmt.Errorf("delete item %q: %w", it.Name, err)
	}
	slug := storedSlug(it)
	if slug == "" || slug != filepath.Base(slug) || !filepath.IsLocal(slug) {
		return fmt.Errorf("delete %s %q: the name gives no file name", it.Kind, it.Name)
	}
	path := l.mainPath(dir, slug)
	if err := refuseLinked(dir, []string{path}); err != nil {
		return fmt.Errorf("delete %s %q: %w", it.Kind, it.Name, err)
	}
	var found folder
	if l.folder {
		if found, err = scanFolder(l.folderPath(dir, slug)); err != nil {
			return fmt.Errorf("delete %s %q: %w", it.Kind, it.Name, err)
		}
	}
	if err := fsx.Remove(dir, path); err != nil {
		return err
	}
	if !l.folder {
		return nil
	}
	root := l.folderPath(dir, slug)
	leftovers := found.junk
	for _, key := range found.files {
		if target := filepath.Join(root, filepath.FromSlash(key)); target != path {
			leftovers = append(leftovers, target)
		}
	}
	for _, target := range leftovers {
		if err := fsx.Remove(dir, target); err != nil {
			return err
		}
	}
	return pruneEmptyDirs(dir, found.dirs)
}

func ContentHash(it Item) string {
	data, err := encodeItem(it)
	if err != nil {
		return ""
	}
	hash := sha256.New()
	hash.Write(fmt.Appendf(nil, "%q %q\n", it.Kind, it.Name))
	hash.Write(fmt.Appendf(nil, "%d\n%s", len(data), data))
	for _, key := range slices.Sorted(maps.Keys(it.Files)) {
		file := it.Files[key]
		hash.Write(fmt.Appendf(nil, "%q %o %d\n%s", key, fileMode(file), len(file.Data), file.Data))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
