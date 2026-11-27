package claudecode

import (
	"path/filepath"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/mdimport"
	"github.com/antonL95/promptstor/internal/mdrules"
)

const (
	boostOwner      = "laravel-boost"
	boostBegin      = "<laravel-boost-guidelines>"
	boostEnd        = "</laravel-boost-guidelines>"
	boostBlockNote  = boostBegin + " block; Boost regenerates it"
	onlyPathsNote   = "ignored by Claude Code; it reads only paths"
	pathsKey        = "paths"
	globsKey        = "globs"
	unreadableFront = "frontmatter %v; not imported"
)

func (im *importer) instructions() error {
	for _, source := range im.instructionSources() {
		resolved, err := mdimport.Resolve(im.t.Env, source, mdimport.Claude)
		if err != nil {
			return err
		}
		for _, file := range resolved.Files {
			doc := im.withoutBoost(file.Path, file.Body)
			for _, it := range mdrules.Split(mdimport.StripImportLines(doc, mdimport.Claude), file.Path) {
				if filepath.Base(file.Path) != agentsName {
					it = im.claudeOnly(it)
				}
				im.out.Items = append(im.out.Items, it)
			}
		}
		im.out.Unresolved = append(im.out.Unresolved, resolved.Unresolved...)
	}
	return nil
}

func (im *importer) instructionSources() []string {
	candidates := []string{im.l.instructions}
	if im.l.scope != adapter.ScopeUser {
		candidates = append(candidates, im.l.in(instructionsName))
	}
	var found []string
	for _, path := range candidates {
		if exists(path) {
			found = append(found, path)
		}
	}
	if agents := filepath.Join(im.l.root, agentsName); len(found) == 0 && im.l.scope != adapter.ScopeUser && exists(agents) {
		found = append(found, agents)
	}
	return found
}

func (im *importer) withoutBoost(path string, doc []byte) []byte {
	lines := strings.SplitAfter(string(doc), "\n")
	begin, end := -1, -1
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case boostBegin:
			if begin < 0 {
				begin = i
			}
		case boostEnd:
			if begin >= 0 && end < 0 {
				end = i
			}
		}
	}
	if begin < 0 || end < 0 {
		return doc
	}
	im.foreign(path, boostOwner, boostBlockNote)
	return []byte(strings.Join(append(lines[:begin:begin], lines[end+1:]...), ""))
}

func (im *importer) ruleFiles() error {
	return im.markdownFiles(im.l.in(rulesDir), func(path, _ string) error {
		it, err := readNative(path, library.KindRule, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
		if err != nil {
			im.note(path, unreadableFront, err)
			return nil
		}
		if _, globs := it.Meta[globsKey]; globs || it.Description != "" {
			im.note(path, onlyPathsNote)
		}
		scope := pathList(it.Meta[pathsKey])
		for _, rule := range mdrules.Split([]byte(it.Body), path) {
			if len(scope) > 0 {
				rule.Activation, rule.Scope = library.ActivationFiles, scope
			}
			im.out.Items = append(im.out.Items, im.claudeOnly(rule))
		}
		return nil
	})
}

func pathList(value any) []string {
	var found []string
	switch typed := value.(type) {
	case string:
		for _, part := range strings.Split(typed, ",") {
			if part = strings.TrimSpace(part); part != "" {
				found = append(found, part)
			}
		}
	case []any:
		for _, entry := range typed {
			if text, ok := entry.(string); ok && strings.TrimSpace(text) != "" {
				found = append(found, strings.TrimSpace(text))
			}
		}
	}
	return found
}
