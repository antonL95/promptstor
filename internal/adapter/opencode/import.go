package opencode

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/mdrules"
)

const (
	noteConfigDirUsed  = "OPENCODE_CONFIG_DIR is set; promptstor uses this folder"
	noteClaudeFallback = "opencode loads this while its AGENTS.md is missing; not imported"
	noteSetting        = "%s is not imported"
	packageOwner       = "opencode packages"
	packageReason      = "package manager state for plugins; opencode manages it"
	pluginsOwner       = "opencode plugins"
	pluginsReason      = "JavaScript plugins; not managed by PromptStor"
	nodeModulesDir     = "node_modules"
	claudeDirName      = ".claude"
	claudeMemoryName   = "CLAUDE.md"
)

var (
	packageEntries    = []string{".gitignore", "bun.lock", nodeModulesDir, "package-lock.json", "package.json"}
	pluginEntries     = []string{"plugin", "plugins"}
	unmanagedSettings = []string{"instructions", "plugin", "plugins", "reference", "references", "skills"}
)

type importer struct {
	t   adapter.Target
	l   layout
	out adapter.Imported
}

func (Adapter) Import(t adapter.Target) (adapter.Imported, error) {
	l, ok := layoutFor(t)
	if !ok {
		return adapter.Imported{}, nil
	}
	im := &importer{t: t, l: l}
	im.configDirNotes()
	documents, err := readDocuments(t.Env, l.configPaths(), func(path, message string) { im.note(path, "%s", message) })
	if err != nil {
		return adapter.Imported{}, err
	}
	steps := []func() error{
		im.rules,
		func() error { return im.agents(documents) },
		func() error { return im.commands(documents) },
		im.skills,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return adapter.Imported{}, err
		}
	}
	im.permissions(documents)
	im.servers(documents)
	im.unmanagedSettings(documents)
	im.foreignState()
	slices.SortStableFunc(im.out.Foreign, func(a, b adapter.Foreign) int { return strings.Compare(a.Path, b.Path) })
	slices.SortStableFunc(im.out.Notes, func(a, b adapter.Note) int { return strings.Compare(a.Path, b.Path) })
	return im.out, nil
}

func (im *importer) note(path, format string, args ...any) {
	im.out.Notes = append(im.out.Notes, adapter.Note{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (im *importer) foreign(path, owner, reason string) {
	im.out.Foreign = append(im.out.Foreign, adapter.Foreign{Path: path, Owner: owner, Reason: reason})
}

func (im *importer) symlinked(path string) {
	target, _ := os.Readlink(path)
	im.note(path, noteSymlinked, target)
}

func (im *importer) read(path string) ([]byte, error) {
	data, problem, err := readFile(im.t.Env, path)
	if problem != "" {
		im.note(path, "%s", problem)
	}
	return data, err
}

func (im *importer) configDirNotes() {
	if im.l.scope != adapter.ScopeUser || variable(im.t.Env, configDirVariable) == "" {
		return
	}
	if im.l.unknownConfig {
		im.note(variable(im.t.Env, configDirVariable), "%s", reasonUnknownLocal)
		return
	}
	im.note(im.l.base, noteConfigDirUsed)
}

func (im *importer) rules() error {
	if im.l.unknownConfig {
		return nil
	}
	path := im.l.at(instructionsName)
	if !exists(path) {
		im.claudeFallback()
		return nil
	}
	data, err := im.read(path)
	if err != nil || data == nil {
		return err
	}
	im.out.Items = append(im.out.Items, mdrules.Split(data, path)...)
	return nil
}

func (im *importer) claudeFallback() {
	if im.l.scope != adapter.ScopeUser {
		return
	}
	if memory := filepath.Join(im.t.Env.Home, claudeDirName, claudeMemoryName); exists(memory) {
		im.note(memory, noteClaudeFallback)
	}
}

func (im *importer) entries(dir string) ([]fs.DirEntry, error) {
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", dir, err)
	case info.Mode()&fs.ModeSymlink != 0 && dir != im.l.sharedSkills():
		im.symlinked(dir)
		return nil, nil
	case info.Mode()&fs.ModeSymlink == 0 && !info.IsDir():
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		im.symlinked(dir)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	return entries, nil
}

func (im *importer) markdownFiles(dir, prefix string, visit func(path, id string) error) error {
	entries, err := im.entries(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		name, isMarkdown := strings.CutSuffix(entry.Name(), markdownExtension)
		switch {
		case strings.HasPrefix(entry.Name(), ".") || entry.Name() == nodeModulesDir:
		case entry.Type()&fs.ModeSymlink != 0:
			im.symlinked(path)
		case entry.IsDir():
			if err := im.markdownFiles(path, prefix+entry.Name()+agentSeparator, visit); err != nil {
				return err
			}
		case isMarkdown && entry.Type().IsRegular():
			if err := visit(path, prefix+name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (im *importer) unmanagedSettings(documents []document) {
	for _, doc := range documents {
		for _, setting := range unmanagedSettings {
			if _, present := doc.fields[setting]; present {
				im.note(doc.path, noteSetting, setting)
			}
		}
	}
}

func (im *importer) foreignState() {
	if im.l.unknownConfig {
		return
	}
	for _, name := range packageEntries {
		if path := im.l.in(name); exists(path) {
			im.foreign(path, packageOwner, packageReason)
		}
	}
	for _, name := range pluginEntries {
		if path := im.l.in(name); exists(path) {
			im.foreign(path, pluginsOwner, pluginsReason)
		}
	}
}
