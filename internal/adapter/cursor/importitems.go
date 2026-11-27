package cursor

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
	noteSubfolder    = "subfolder; not imported"
	noteExtension    = "not a .md file; not imported"
	noteAlsoDefined  = "skill %s is also defined in %s; not imported"
	noteNoSkillFile  = "no SKILL.md here; skills in category folders are not imported"
)

func (im *importer) markdownItems(dir string, kind library.Kind, convert func(path string, it library.Item) library.Item) error {
	subfolder := func(path string) error {
		im.note(path, noteSubfolder)
		return nil
	}
	return im.walk(dir, subfolder, func(path string, entry fs.DirEntry) error {
		name, isMarkdown := strings.CutSuffix(entry.Name(), markdownExtension)
		if !isMarkdown {
			im.note(path, noteExtension)
			return nil
		}
		data, err := im.read(path)
		if err != nil || data == nil {
			return err
		}
		it, err := library.ParseMarkdown(kind, name, managed.StripNotice(data))
		if err != nil {
			im.note(path, noteFrontmatter, err)
			return nil
		}
		im.out.Items = append(im.out.Items, convert(path, it))
		return nil
	})
}

func (im *importer) commands() error {
	return im.markdownItems(im.l.in(commandsDir), library.KindCommand, func(path string, it library.Item) library.Item {
		for _, key := range slices.Sorted(maps.Keys(it.Meta)) {
			im.note(path, noteFieldSkipped, key)
		}
		name := strings.TrimSuffix(filepath.Base(path), markdownExtension)
		if it.Name != name {
			im.note(path, noteFieldSkipped, nameKey)
		}
		return library.Item{Kind: library.KindCommand, Name: name, Description: it.Description, Body: it.Body, Source: path}
	})
}

func (im *importer) subagents() error {
	return im.markdownItems(im.l.in(agentsDir), library.KindSubagent, func(path string, it library.Item) library.Item {
		meta := map[string]any{}
		for _, key := range slices.Sorted(maps.Keys(it.Meta)) {
			at := slices.IndexFunc(agentFields, func(field agentField) bool { return field.name == key })
			if at >= 0 && agentFields[at].accepts(it.Meta[key]) {
				meta[agentFields[at].meta] = it.Meta[key]
			} else {
				im.note(path, noteFieldSkipped, key)
			}
		}
		agent := library.Item{Kind: library.KindSubagent, Name: it.Name, Description: it.Description, Body: it.Body, Source: path}
		if len(meta) > 0 {
			agent.Meta = meta
		}
		return agent
	})
}

func (im *importer) skills() error {
	found := map[string]string{}
	for _, dir := range []string{im.l.sharedSkills(), im.l.in(skillsDir)} {
		entries, err := im.entries(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			earlier, taken := found[entry.Name()]
			switch {
			case entry.Type()&fs.ModeSymlink != 0:
				im.symlinked(path)
			case !entry.IsDir():
			case !exists(filepath.Join(path, skillFile)):
				im.note(path, noteNoSkillFile)
			case taken:
				im.note(path, noteAlsoDefined, entry.Name(), earlier)
			default:
				found[entry.Name()] = path
				if err := im.skill(path); err != nil {
					return err
				}
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
	data, err := im.read(main)
	if err != nil || data == nil {
		return err
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
