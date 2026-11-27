package codex

import (
	"bytes"
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
	noteUnreadable    = "%v; not imported"
	noteFrontmatter   = "frontmatter %v; not imported"
	noteDeadPrompt    = "no longer loaded by Codex"
	noteStraySkills   = "not a documented skill folder; not imported"
	noteCredential    = "matches the credential exclusion list; not imported"
	noteInvalidTOML   = "invalid TOML; not imported"
	noteAgentRequired = "needs name, description and developer_instructions; not imported"
	noteAgentField    = "field %s not imported"
	markdownExtension = ".md"
	tomlExtension     = ".toml"
)

func (im *importer) sharedSkills() error {
	return im.skillsIn(im.l.sharedSkills(), func(it library.Item, _ bool) {
		im.out.Items = append(im.out.Items, it)
	})
}

func (im *importer) legacySkills() error {
	return im.skillsIn(im.l.in(skillsDir), func(it library.Item, generated bool) {
		if generated && len(it.Files) == 0 {
			it.Kind = library.KindCommand
			it.Source = filepath.Join(it.Source, skillFile)
		}
		im.out.Items = append(im.out.Items, it)
	})
}

func (im *importer) projectCodexSkills() error {
	if path := im.l.in(skillsDir); exists(path) {
		im.note(path, noteStraySkills)
	}
	return nil
}

func (im *importer) skillsIn(dir string, add func(it library.Item, generated bool)) error {
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
			if err := im.skill(path, add); err != nil {
				return err
			}
		}
	}
	return nil
}

func (im *importer) skill(dir string, add func(it library.Item, generated bool)) error {
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
	add(it, bytes.Contains(data, []byte(managed.Notice)))
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

func (im *importer) prompts() error {
	return im.files(im.l.in(promptsDir), markdownExtension, func(path, file string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		name := strings.TrimSuffix(file, markdownExtension)
		it, err := library.ParseMarkdown(library.KindCommand, name, managed.StripNotice(data))
		if err != nil {
			im.note(path, noteFrontmatter, err)
			return nil
		}
		it.Name, it.Source = name, path
		im.note(path, noteDeadPrompt)
		im.out.Items = append(im.out.Items, it)
		return nil
	})
}

func (im *importer) subagents() error {
	return im.files(im.l.in(agentsDir), tomlExtension, func(path, _ string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		fields, ok := decodeTable(data)
		if !ok {
			im.note(path, noteInvalidTOML)
			return nil
		}
		name, _ := fields[agentName].(string)
		description, _ := fields[agentDescription].(string)
		instructions, _ := fields[agentInstructions].(string)
		if name == "" || description == "" || strings.TrimSpace(instructions) == "" {
			im.note(path, noteAgentRequired)
			return nil
		}
		it := library.Item{Kind: library.KindSubagent, Name: name, Description: description, Body: instructions, Source: path}
		for _, key := range slices.Sorted(maps.Keys(fields)) {
			if key == agentName || key == agentDescription || key == agentInstructions {
				continue
			}
			meta, known := metaKeyOf(key)
			value, text := fields[key].(string)
			if !known || !text {
				im.note(path, noteAgentField, key)
				continue
			}
			if it.Meta == nil {
				it.Meta = map[string]any{}
			}
			it.Meta[meta] = value
		}
		im.out.Items = append(im.out.Items, it)
		return nil
	})
}
