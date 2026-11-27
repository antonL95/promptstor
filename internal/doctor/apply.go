package doctor

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/preview"
)

const questionApply = "Apply these changes?"

var applyOptions = []string{"yes", "no", "preview"}

func (d *doctor) printPreview(s *firstRunState, p engine.Plan) error {
	d.w.printf("\nPreview of the first sync:\n")
	for _, m := range s.moves {
		if m.after != nil {
			d.w.printf("move rules into the managed section:\n%s", preview.UnifiedDiff(m.path, m.before, m.after))
		}
	}
	for _, del := range s.adoption.deletions {
		if del.wanted() {
			d.w.printf("delete %s\n", del.path)
		}
	}
	if d.w.err != nil {
		return d.w.done()
	}
	if _, err := preview.Plan(d.w.w, p, true); err != nil {
		return err
	}
	d.printNotes(p)
	return nil
}

func (d *doctor) confirmApply(s *firstRunState, p engine.Plan) (bool, error) {
	def := 1
	if d.opt.Yes {
		def = 0
	}
	for {
		at, err := d.p.Select(questionApply, applyOptions, def)
		if err != nil {
			return false, err
		}
		if applyOptions[at] != "preview" {
			return applyOptions[at] == "yes", nil
		}
		if err := d.printPreview(s, p); err != nil {
			return false, err
		}
	}
}

func (d *doctor) applyFiles(s *firstRunState) error {
	for _, m := range s.moves {
		if m.after == nil {
			continue
		}
		current, err := os.ReadFile(m.path)
		if err != nil {
			return fmt.Errorf("read %s: %w", m.path, err)
		}
		if !bytes.Equal(current, m.before) {
			return fmt.Errorf("%s changed since the preview; run promptstor doctor again", m.path)
		}
		if err := fsx.WriteAtomic(m.root, m.path, m.after, m.mode); err != nil {
			return err
		}
		d.w.printf("moved rules out of %s\n", m.path)
	}
	for _, del := range s.adoption.deletions {
		if !del.wanted() {
			continue
		}
		credential, err := excludedInside(d.e.Env, del.path)
		if err != nil {
			return err
		}
		if credential != "" {
			d.w.printf("kept %s: it holds %s, which PromptStor never backs up or deletes\n", del.path, credential)
			continue
		}
		if err := removeOriginal(del.root, del.path); err != nil {
			return err
		}
		d.w.printf("deleted %s\n", del.path)
	}
	return nil
}

func excludedInside(env paths.Env, path string) (string, error) {
	entry, err := fsx.Inspect(path)
	if err != nil || !entry.Exists || entry.IsLink {
		return "", err
	}
	var found string
	err = filepath.WalkDir(path, func(current string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !de.IsDir() && backup.Excluded(env, current) {
			found = current
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", path, err)
	}
	return found, nil
}

func removeOriginal(root, path string) error {
	link, linked, err := fsx.LinkedAncestor(root, path)
	if err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	if linked {
		return fmt.Errorf("remove %s: %w: %s", path, fsx.ErrSymlink, link)
	}
	entry, err := fsx.Inspect(path)
	if err != nil {
		return err
	}
	if !entry.Exists {
		return nil
	}
	if !entry.IsDir {
		return fsx.Remove(root, path)
	}
	if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
