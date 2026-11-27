package opencode

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
	noteFieldSkipped  = "%sfield %s not imported"
	noteFrontmatter   = "frontmatter %v; not imported"
	noteBuiltinAgent  = "%soverrides the built in agent %q; not imported"
	noteNoPrompt      = "%shas no prompt; not imported"
	noteSubstitution  = "%suses {env:...} or {file:...}; not imported"
	noteAlsoDefined   = "%sis also defined in %s, which is imported"
	noteDefinition    = "%sis not an object; not imported"
	promptKey         = "prompt"
	systemKey         = "system"
	templateKey       = "template"
	legacyStepsKey    = "maxSteps"
	legacyDisabledKey = "disable"
)

var (
	nativeAgentKeys = []string{permissionsKey, "request", systemKey, disabledKey}
	knownAgentKeys  = []string{"color", descriptionKey, disabledKey, "hidden", modeKey, modelKey, permissionsKey, "request", stepsKey, systemKey, variantKey}
)

type definition struct {
	name, path, label string
	body              string
	fields            map[string]any
	legacy, primary   bool
	inConfig          bool
}

type definitions map[string]library.Item

func (im *importer) agents(documents []document) error {
	found := definitions{}
	for _, doc := range documents {
		for _, key := range []string{"agent", "mode", "agents"} {
			im.configured(doc, key, func(d definition) {
				d.legacy, d.primary = key != "agents", key == "mode"
				d.body = text(d.fields[systemKey])
				if d.legacy {
					d.body = text(d.fields[promptKey])
				}
				im.agent(found, d)
			})
		}
	}
	for _, dir := range []string{legacyAgentsDir, agentsDir, legacyModesDir, modesDir} {
		if im.l.unknownConfig {
			break
		}
		primary := dir == modesDir || dir == legacyModesDir
		err := im.markdownFiles(im.l.in(dir), "", func(path, id string) error {
			if primary && strings.Contains(id, agentSeparator) {
				return nil
			}
			return im.markdownDefinition(library.KindSubagent, path, id, func(d definition) {
				d.primary = primary
				d.legacy = isLegacyAgent(d.fields)
				im.agent(found, d)
			})
		})
		if err != nil {
			return err
		}
	}
	im.add(found)
	return nil
}

func isLegacyAgent(fields map[string]any) bool {
	unknown := false
	for key := range fields {
		if slices.Contains(nativeAgentKeys, key) {
			return false
		}
		unknown = unknown || !slices.Contains(knownAgentKeys, key)
	}
	return unknown
}

func (im *importer) configured(doc document, key string, visit func(definition)) {
	entries := object(doc.fields[key])
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		label := key + " " + name + ": "
		fields := object(plain(entries[name]))
		if fields == nil {
			im.note(doc.path, noteDefinition, label)
			continue
		}
		visit(definition{name: name, path: doc.path, label: label, fields: fields, inConfig: true})
	}
}

func (im *importer) markdownDefinition(kind library.Kind, path, id string, visit func(definition)) error {
	data, err := im.read(path)
	if err != nil || data == nil {
		return err
	}
	it, err := library.ParseMarkdown(kind, "", managed.StripNotice(data))
	if err != nil {
		im.note(path, noteFrontmatter, err)
		return nil
	}
	fields := maps.Clone(it.Meta)
	if fields == nil {
		fields = map[string]any{}
	}
	if it.Description != "" {
		fields[descriptionKey] = it.Description
	}
	if it.Name != "" {
		fields[nameKey] = it.Name
	}
	visit(definition{name: id, path: path, body: it.Body, fields: fields})
	return nil
}

func (im *importer) usable(d definition) bool {
	if d.inConfig && (strings.Contains(d.body, "{env:") || strings.Contains(d.body, "{file:")) {
		im.note(d.path, noteSubstitution, d.label)
		return false
	}
	return true
}

func joinVariant(fields map[string]any) map[string]any {
	model, variant := text(fields[modelKey]), text(fields[variantKey])
	if model == "" || variant == "" || strings.Contains(model, "#") || strings.Contains(variant, "#") {
		return fields
	}
	joined := maps.Clone(fields)
	joined[modelKey] = model + "#" + variant
	delete(joined, variantKey)
	return joined
}

func (im *importer) agent(found definitions, d definition) {
	switch {
	case slices.Contains(builtinAgents, d.name):
		im.note(d.path, noteBuiltinAgent, d.label, d.name)
		return
	case strings.TrimSpace(d.body) == "":
		im.note(d.path, noteNoPrompt, d.label)
		return
	case !im.usable(d):
		return
	}
	fields := joinVariant(d.fields)
	if d.legacy {
		fields = renamed(fields, map[string]string{legacyStepsKey: stepsKey, legacyDisabledKey: disabledKey})
	}
	if d.primary {
		fields = maps.Clone(fields)
		fields[modeKey] = primaryMode
	}
	meta := map[string]any{}
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		native, known := fieldNamed(agentFields, key)
		switch {
		case key == descriptionKey || d.inConfig && (key == systemKey && !d.legacy || key == promptKey && d.legacy):
		case known && (!d.legacy || key != permissionsKey) && native.accepts(fields[key]):
			meta[native.meta] = fields[key]
		default:
			im.note(d.path, noteFieldSkipped, d.label, key)
		}
	}
	switch _, hasMode := fields[modeKey]; {
	case !hasMode:
		meta[modeMeta] = primaryMode
	case meta[modeMeta] == subagentMode:
		delete(meta, modeMeta)
	}
	im.define(found, library.KindSubagent, d, meta)
}

func renamed(fields map[string]any, names map[string]string) map[string]any {
	out := maps.Clone(fields)
	for old, current := range names {
		value, present := out[old]
		if _, taken := out[current]; present && !taken {
			out[current] = value
			delete(out, old)
		}
	}
	return out
}

func (im *importer) define(found definitions, kind library.Kind, d definition, meta map[string]any) {
	if earlier, taken := found[d.name]; taken {
		im.note(earlier.Source, noteAlsoDefined, string(kind)+" "+d.name+" ", d.path)
	}
	it := library.Item{Kind: kind, Name: d.name, Description: text(d.fields[descriptionKey]), Body: d.body, Source: d.path}
	if kind == library.KindCommand {
		it.Name = strings.ReplaceAll(d.name, agentSeparator, commandSeparator)
	}
	if len(meta) > 0 {
		it.Meta = meta
	}
	found[d.name] = it
}

func (im *importer) add(found definitions) {
	for _, name := range slices.Sorted(maps.Keys(found)) {
		im.out.Items = append(im.out.Items, found[name])
	}
}

func (im *importer) commands(documents []document) error {
	found := definitions{}
	for _, doc := range documents {
		for _, key := range []string{"command", "commands"} {
			im.configured(doc, key, func(d definition) {
				d.body = text(d.fields[templateKey])
				im.command(found, d)
			})
		}
	}
	for _, dir := range []string{legacyCommandsDir, commandsDir} {
		if im.l.unknownConfig {
			break
		}
		err := im.markdownFiles(im.l.in(dir), "", func(path, id string) error {
			return im.markdownDefinition(library.KindCommand, path, id, func(d definition) {
				im.command(found, d)
			})
		})
		if err != nil {
			return err
		}
	}
	im.add(found)
	return nil
}

func (im *importer) command(found definitions, d definition) {
	switch {
	case strings.TrimSpace(d.body) == "":
		im.note(d.path, noteNoPrompt, d.label)
		return
	case !im.usable(d):
		return
	}
	fields := renamed(joinVariant(d.fields), map[string]string{subtaskKey: subagentKey})
	meta := map[string]any{}
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		native, known := fieldNamed(commandFields, key)
		switch {
		case key == descriptionKey || d.inConfig && key == templateKey:
		case known && native.accepts(fields[key]):
			meta[native.meta] = fields[key]
		default:
			im.note(d.path, noteFieldSkipped, d.label, key)
		}
	}
	im.define(found, library.KindCommand, d, meta)
}

func (im *importer) skills() error {
	dirs := []string{im.l.sharedSkills()}
	if !im.l.unknownConfig {
		dirs = append(dirs, im.l.in(legacySkillsDir), im.l.in(skillsDir))
	}
	found := definitions{}
	for _, dir := range dirs {
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
				if err := im.skill(found, path); err != nil {
					return err
				}
			}
		}
	}
	im.add(found)
	return nil
}

func (im *importer) skill(found definitions, dir string) error {
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
	if earlier, taken := found[filepath.Base(dir)]; taken {
		im.note(earlier.Source, noteAlsoDefined, "skill "+filepath.Base(dir)+" ", dir)
	}
	found[filepath.Base(dir)] = it
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
