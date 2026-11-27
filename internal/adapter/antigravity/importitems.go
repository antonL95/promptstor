package antigravity

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
)

const (
	noteFieldSkipped = "field %s not imported"
	noteCredential   = "matches the credential exclusion list; not imported"
)

func (im *importer) subagents() error {
	dir := im.l.in(agentsDir)
	if err := im.markdownFiles(dir, im.subagent); err != nil {
		return err
	}
	entries, err := im.entries(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name(), agentFile)
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !exists(path) {
			continue
		}
		if err := im.subagent(path, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (im *importer) subagent(path, name string) error {
	data, found, err := im.read(path)
	if err != nil || !found {
		return err
	}
	it, err := library.ParseMarkdown(library.KindSubagent, name, managed.StripNotice(data))
	if err != nil {
		im.note(path, noteFrontmatter, err)
		return nil
	}
	meta := map[string]any{}
	for _, key := range slices.Sorted(maps.Keys(it.Meta)) {
		if field, known := agentFieldNamed(key); known && field.accepts(it.Meta[key]) {
			meta[field.meta] = it.Meta[key]
		} else {
			im.note(path, noteFieldSkipped, key)
		}
	}
	agent := library.Item{Kind: library.KindSubagent, Name: it.Name, Description: it.Description, Body: it.Body, Source: path}
	if len(meta) > 0 {
		agent.Meta = meta
	}
	im.out.Items = append(im.out.Items, agent)
	return nil
}

func (im *importer) skills() error {
	dir := im.l.in(skillsDir)
	entries, err := im.entries(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		switch {
		case strings.HasPrefix(entry.Name(), "."):
		case entry.Type()&fs.ModeSymlink != 0:
			im.symlinked(path)
		case entry.IsDir():
			if err := im.skill(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (im *importer) skill(dir string) error {
	main := filepath.Join(dir, skillFile)
	if entry, err := os.Lstat(main); err != nil || !entry.Mode().IsRegular() {
		return nil
	}
	data, err := os.ReadFile(main)
	if err != nil {
		return fmt.Errorf("read %s: %w", main, err)
	}
	it, err := library.ParseMarkdown(library.KindSkill, filepath.Base(dir), managed.StripNotice(data))
	if err != nil {
		im.note(main, noteFrontmatter, err)
		return nil
	}
	it.Source = dir
	if it.Files, err = im.attachments(dir); err != nil {
		return err
	}
	im.out.Items = append(im.out.Items, it)
	return nil
}

func (im *importer) attachments(dir string) (map[string]library.File, error) {
	var files map[string]library.File
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		switch {
		case path == dir:
			return nil
		case strings.HasPrefix(entry.Name(), "."):
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case entry.IsDir():
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		switch {
		case relative == skillFile:
			return nil
		case entry.Type()&fs.ModeSymlink != 0:
			im.symlinked(path)
			return nil
		case !entry.Type().IsRegular():
			return nil
		case backup.Excluded(im.t.Env, path):
			im.note(path, noteCredential)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if files == nil {
			files = map[string]library.File{}
		}
		files[filepath.ToSlash(relative)] = library.File{Data: data, Mode: info.Mode().Perm()}
		return nil
	})
	return files, err
}
