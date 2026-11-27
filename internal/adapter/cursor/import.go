package cursor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
)

const (
	noteInvalidJSON       = "invalid JSON; not imported"
	noteUserRules         = "only self hosted Cursor workers are documented to load these; not imported"
	noteLegacyRules       = "legacy .cursorrules file; not imported"
	noteEditorPermissions = "editor allow lists; not imported"
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
		im.rules,
		im.commands,
		im.subagents,
		im.skills,
		im.permissions,
		im.hooks,
		im.servers,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return adapter.Imported{}, err
		}
	}
	im.unmanaged()
	slices.SortStableFunc(im.out.Notes, func(a, b adapter.Note) int { return strings.Compare(a.Path, b.Path) })
	return im.out, nil
}

func (im *importer) note(path, format string, args ...any) {
	im.out.Notes = append(im.out.Notes, adapter.Note{Path: path, Message: fmt.Sprintf(format, args...)})
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

func (im *importer) object(path string) (map[string]json.RawMessage, error) {
	data, err := im.read(path)
	if err != nil || data == nil {
		return nil, err
	}
	fields, valid := decodeObject(data)
	if !valid {
		im.note(path, noteInvalidJSON)
	}
	return fields, nil
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
	return slices.DeleteFunc(entries, func(entry fs.DirEntry) bool { return strings.HasPrefix(entry.Name(), ".") }), nil
}

func (im *importer) walk(dir string, subfolder func(path string) error, file func(path string, entry fs.DirEntry) error) error {
	entries, err := im.entries(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			im.symlinked(path)
		case entry.IsDir():
			err = subfolder(path)
		case entry.Type().IsRegular():
			err = file(path, entry)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (im *importer) unmanaged() {
	notePresent := func(path, message string) {
		if exists(path) {
			im.note(path, "%s", message)
		}
	}
	notePresent(im.l.in(editorPermissions), noteEditorPermissions)
	if im.l.scope == adapter.ScopeUser {
		notePresent(im.l.in(rulesDir), noteUserRules)
		return
	}
	notePresent(filepath.Join(im.l.root, legacyRulesName), noteLegacyRules)
}
