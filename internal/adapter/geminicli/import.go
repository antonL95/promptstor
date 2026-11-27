package geminicli

import (
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
	noteSymlinked      = "symlinked -> %s; not imported"
	noteHomeOverride   = "GEMINI_CLI_HOME is set; promptstor reads only ~/.gemini"
	noteSavedPolicy    = "written by Gemini CLI; not read"
	noteOtherPolicy    = "policy file PromptStor does not manage; not imported"
	noteProjectPolicy  = "project policy files are ignored by Gemini CLI"
	extensionsOwner    = "gemini-cli extensions"
	extensionsReason   = "installed extensions; Gemini CLI manages them"
	antigravityOwner   = "antigravity"
	antigravityReason  = "belongs to Antigravity, not Gemini CLI"
	runtimeOwner       = "gemini-cli runtime"
	runtimeReason      = "runtime state; Gemini CLI rewrites it"
	antigravityPattern = "antigravity*"
	antigravityConfig  = "config"
	runtimeDir         = "tmp"
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
		im.settings,
		im.policies,
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

func (im *importer) symlinked(path string) {
	target, _ := os.Readlink(path)
	im.note(path, noteSymlinked, target)
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

func (im *importer) policies() error {
	return im.files(im.l.in(policiesDir), tomlExtension, func(path, name string) error {
		switch {
		case im.l.scope != adapter.ScopeUser:
			im.note(path, noteProjectPolicy)
		case name == savedPolicy:
			im.note(path, noteSavedPolicy)
		case name != ownPolicy:
			im.note(path, noteOtherPolicy)
		}
		return nil
	})
}

func (im *importer) foreignState() error {
	if im.l.scope != adapter.ScopeUser {
		return nil
	}
	if im.t.Env.Getenv != nil && im.t.Env.Getenv(homeVariable) != "" {
		im.note(im.l.config, noteHomeOverride)
	}
	entries, err := im.entries(im.l.config)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := im.l.in(entry.Name())
		antigravity, _ := filepath.Match(antigravityPattern, entry.Name())
		switch {
		case antigravity || entry.Name() == antigravityConfig:
			im.foreign(path, antigravityOwner, antigravityReason)
		case entry.Name() == extensionsDir:
			im.foreign(path, extensionsOwner, extensionsReason)
		case entry.Name() == runtimeDir:
			im.foreign(path, runtimeOwner, runtimeReason)
		}
	}
	return nil
}
