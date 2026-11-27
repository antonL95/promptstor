package geminicli

import (
	"os"
	"path/filepath"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/mdimport"
	"github.com/antonL95/promptstor/internal/mdrules"
)

const noteUnreadableMemory = "not a readable file; rules not imported"

func (im *importer) rules() error {
	source := filepath.Join(im.l.root, memoryName)
	if im.l.scope == adapter.ScopeUser {
		memory, reason := im.l.memoryFile(im.t.Env)
		if reason != "" {
			im.note(im.l.settings, "%s; rules not imported", reason)
			return nil
		}
		source = memory
	}
	if !exists(source) {
		return nil
	}
	if info, err := os.Stat(source); err != nil || !info.Mode().IsRegular() {
		im.note(source, noteUnreadableMemory)
		return nil
	}
	resolved, err := mdimport.Resolve(im.t.Env, source, mdimport.Gemini)
	if err != nil {
		return err
	}
	for _, file := range resolved.Files {
		for _, it := range mdrules.Split(mdimport.StripImportLines(file.Body, mdimport.Gemini), file.Path) {
			if im.l.scope == adapter.ScopeProject && filepath.Base(file.Path) != agentsName {
				it.Targets = library.Targets{Include: []string{tool}}
			}
			im.out.Items = append(im.out.Items, it)
		}
	}
	im.out.Unresolved = append(im.out.Unresolved, resolved.Unresolved...)
	return nil
}
