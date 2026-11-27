package codex

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
	noteSymlinked  = "symlinked -> %s; not imported"
	noteOverride   = "overrides AGENTS.md; PromptStor rules are not loaded"
	noteProfile    = "profile file; not imported"
	noteOtherRules = "rules file PromptStor does not manage; not imported"
	pluginsOwner   = "codex plugins"
	pluginsReason  = "installed plugins; Codex manages them"
	systemOwner    = "codex"
	systemReason   = "bundled skills; Codex rewrites them"
	rulesExtension = ".rules"
	commentPrefix  = "#"
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
	steps := []func() error{
		im.override,
		im.instructions,
		im.subagents,
		im.sharedSkills,
		im.hooks,
		im.config,
		im.rules,
	}
	if l.scope == adapter.ScopeUser {
		steps = append(steps, im.legacySkills, im.prompts, im.profiles, im.plugins)
	} else {
		steps = append(steps, im.projectCodexSkills)
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return adapter.Imported{}, err
		}
	}
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

func (im *importer) read(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	return data, true, nil
}

func (im *importer) override() error {
	path := filepath.Join(filepath.Dir(im.l.instructions), overrideName)
	data, found, err := im.read(path)
	if err != nil || !found || strings.TrimSpace(string(data)) == "" {
		return err
	}
	im.note(path, noteOverride)
	return nil
}

func (im *importer) instructions() error {
	if im.l.scope != adapter.ScopeUser {
		return nil
	}
	data, found, err := im.read(im.l.instructions)
	if err != nil || !found {
		return err
	}
	im.out.Items = append(im.out.Items, mdrules.Split(data, im.l.instructions)...)
	return nil
}

func (im *importer) rules() error {
	return im.files(im.l.in(rulesDir), rulesExtension, func(path, name string) error {
		switch {
		case name == ownRules:
			return nil
		case name == defaultRules && im.l.scope == adapter.ScopeUser:
			return im.grants(path)
		default:
			im.note(path, noteOtherRules)
			return nil
		}
	})
}

func (im *importer) grants(path string) error {
	data, found, err := im.read(path)
	if err != nil || !found {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, commentPrefix) {
			im.out.Grants = append(im.out.Grants, adapter.Grant{Tool: tool, Path: path, Key: line, Value: line})
		}
	}
	return nil
}

func (im *importer) profiles() error {
	return im.files(im.l.config, profileSuffix, func(path, _ string) error {
		im.note(path, noteProfile)
		return nil
	})
}

func (im *importer) plugins() error {
	if path := im.l.in(pluginsDir); exists(path) {
		im.foreign(path, pluginsOwner, pluginsReason)
	}
	if path := im.l.in(skillsDir, systemDir); exists(path) {
		im.foreign(path, systemOwner, systemReason)
	}
	return nil
}

func (im *importer) entries(dir string) ([]fs.DirEntry, error) {
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", dir, err)
	case info.Mode()&fs.ModeSymlink != 0 && dir != im.l.config && dir != im.l.sharedSkills():
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

func (im *importer) files(dir, suffix string, visit func(path, name string) error) error {
	entries, err := im.entries(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		switch {
		case strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(entry.Name(), suffix):
		case entry.Type()&fs.ModeSymlink != 0:
			im.symlinked(path)
		case entry.Type().IsRegular():
			if err := visit(path, entry.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}
