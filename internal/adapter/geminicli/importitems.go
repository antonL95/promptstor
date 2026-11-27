package geminicli

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
)

const (
	commandSeparator  = ":"
	promptKey         = "prompt"
	descriptionKey    = "description"
	kindKey           = "kind"
	localKind         = "local"
	remoteKind        = "remote"
	ignoredPrefix     = "_"
	noteInvalidTOML   = "invalid TOML; not imported"
	noteNoPrompt      = "no prompt; not imported"
	noteFieldSkipped  = "field %s not imported"
	noteFrontmatter   = "frontmatter %v; not imported"
	noteRemoteAgent   = "remote agent; not imported"
	noteShadowedSkill = "shadowed by .agents/skills; not imported"
	noteCredential    = "matches the credential exclusion list; not imported"
)

func (im *importer) commands() error {
	return im.commandsIn(im.l.in(commandsDir), "")
}

func (im *importer) commandsIn(dir, prefix string) error {
	entries, err := im.entries(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		name, isCommand := strings.CutSuffix(entry.Name(), tomlExtension)
		switch {
		case strings.HasPrefix(entry.Name(), "."):
		case entry.Type()&fs.ModeSymlink != 0:
			im.symlinked(path)
		case entry.IsDir():
			if err := im.commandsIn(path, prefix+entry.Name()+commandSeparator); err != nil {
				return err
			}
		case isCommand && entry.Type().IsRegular():
			if err := im.command(path, prefix+name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (im *importer) command(path, name string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var fields map[string]any
	if err := toml.Unmarshal(data, &fields); err != nil {
		im.note(path, noteInvalidTOML)
		return nil
	}
	prompt, ok := fields[promptKey].(string)
	if !ok {
		im.note(path, noteNoPrompt)
		return nil
	}
	description, _ := fields[descriptionKey].(string)
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		if key != promptKey && key != descriptionKey {
			im.note(path, noteFieldSkipped, key)
		}
	}
	im.out.Items = append(im.out.Items, library.Item{Kind: library.KindCommand, Name: name, Description: description, Body: prompt, Source: path})
	return nil
}

func (im *importer) subagents() error {
	return im.files(im.l.in(agentsDir), markdownExtension, func(path, file string) error {
		if strings.HasPrefix(file, ignoredPrefix) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		it, err := library.ParseMarkdown(library.KindSubagent, strings.TrimSuffix(file, markdownExtension), managed.StripNotice(data))
		if err != nil {
			im.note(path, noteFrontmatter, err)
			return nil
		}
		if isRemoteAgent(it.Meta) {
			im.note(path, noteRemoteAgent)
			return nil
		}
		meta := map[string]any{}
		for _, key := range slices.Sorted(maps.Keys(it.Meta)) {
			field, known := agentFieldNamed(key)
			switch {
			case known:
				meta[field.meta] = it.Meta[key]
			case key != kindKey || it.Meta[key] != localKind:
				im.note(path, noteFieldSkipped, key)
			}
		}
		agent := library.Item{Kind: library.KindSubagent, Name: it.Name, Description: it.Description, Body: it.Body, Source: path}
		if len(meta) > 0 {
			agent.Meta = meta
		}
		im.out.Items = append(im.out.Items, agent)
		return nil
	})
}

func isRemoteAgent(meta map[string]any) bool {
	_, cardURL := meta["agent_card_url"]
	_, cardJSON := meta["agent_card_json"]
	return meta[kindKey] == remoteKind || cardURL || cardJSON
}

func (im *importer) skills() error {
	shared := map[string]bool{}
	if err := im.skillsIn(im.l.sharedSkills(), func(it library.Item) {
		shared[it.Name] = true
		im.out.Items = append(im.out.Items, it)
	}); err != nil {
		return err
	}
	return im.skillsIn(im.l.in(skillsDir), func(it library.Item) {
		if shared[it.Name] {
			im.note(it.Source, noteShadowedSkill)
			return
		}
		im.out.Items = append(im.out.Items, it)
	})
}

func (im *importer) skillsIn(dir string, add func(library.Item)) error {
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

func (im *importer) skill(dir string, add func(library.Item)) error {
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
	add(it)
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
