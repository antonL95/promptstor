package claudecode

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
)

const (
	noteSymlinked  = "symlinked -> %s; not imported"
	noteUnreadable = "%v; not imported"
	noteConfigDir  = "CLAUDE_CONFIG_DIR is set; promptstor reads only ~/.claude and ~/.claude.json"
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
		im.instructions,
		im.ruleFiles,
		im.commands,
		im.subagents,
		im.skills,
		im.settings,
		im.localSettings,
		im.servers,
		im.foreignState,
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

func (im *importer) claudeOnly(it library.Item) library.Item {
	if im.l.scope == adapter.ScopeProject {
		it.Targets = library.Targets{Include: []string{tool}}
	}
	return it
}

func (im *importer) markdownFiles(dir string, visit func(path, relative string) error) error {
	return walkFiles(dir, func(path, relative string, entry fs.DirEntry) error {
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(path)
			im.note(path, noteSymlinked, target)
			return nil
		case !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(path), ".md"):
			return nil
		}
		return visit(path, relative)
	})
}

func walkFiles(dir string, visit func(path, relative string, entry fs.DirEntry) error) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if path == dir {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		return visit(path, filepath.ToSlash(relative), entry)
	})
}

func readNative(path string, kind library.Kind, name string) (library.Item, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return library.Item{}, fmt.Errorf("read %s: %w", path, err)
	}
	it, err := library.ParseMarkdown(kind, name, managed.StripNotice(data))
	if err != nil {
		return library.Item{}, fmt.Errorf("parse %s: %w", path, err)
	}
	it.Source = path
	return it, nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
