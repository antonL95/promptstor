package antigravity

import (
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/mdimport"
	"github.com/antonL95/promptstor/internal/mdrules"
)

const (
	reasonUnconfirmed   = "unconfirmed syntax"
	noteShared          = "shared with Gemini CLI"
	noteUnreadableRules = "not a readable file; rules not imported"
	noteManualRule      = "manual rules have no PromptStor activation; not imported"
	noteBadTrigger      = "Antigravity discards a rule without a valid trigger; not imported"
	noteBadGlobs        = "globs is not a comma separated string without braces; not imported"
	noteFrontmatter     = "frontmatter %v; not imported"
	noteRuleKey         = "frontmatter key %s not imported"
)

var labelledImports = mdimport.Syntax{
	Name: tool,
	Line: regexp.MustCompile(`(?:^|\s)@(?P<path>\[[^\]]*\]\([^)]*\))`),
}

func (im *importer) rules() error {
	err := im.markdownFiles(im.l.in(rulesDir), im.modularRule)
	if err != nil || im.l.scope != adapter.ScopeUser {
		return err
	}
	shared := im.l.gemini(memoryName)
	for _, source := range []string{im.l.in(memoryName), im.l.in(agentsName), shared, im.l.gemini(agentsName)} {
		found, err := im.memory(source)
		if err != nil {
			return err
		}
		if found && source == shared {
			im.note(source, noteShared)
		}
	}
	return nil
}

func (im *importer) memory(path string) (bool, error) {
	if !exists(path) {
		return false, nil
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		im.note(path, noteUnreadableRules)
		return false, nil
	}
	data, found, err := im.read(path)
	if err != nil || !found {
		return false, err
	}
	im.unresolved(path, data)
	im.out.Items = append(im.out.Items, mdrules.Split(data, path)...)
	return true, nil
}

func (im *importer) unresolved(path string, data []byte) {
	for _, found := range mdimport.Find(data, labelledImports) {
		im.out.Unresolved = append(im.out.Unresolved, adapter.Unresolved{
			File: path, Line: strconv.Itoa(found.Line), Target: found.Token, Reason: reasonUnconfirmed,
		})
	}
}

func (im *importer) modularRule(path, name string) error {
	data, found, err := im.read(path)
	if err != nil || !found {
		return err
	}
	parsed, err := library.ParseMarkdown(library.KindRule, name, managed.StripNotice(data))
	if err != nil {
		im.note(path, noteFrontmatter, err)
		return nil
	}
	activation, scope, reason := activationOf(parsed.Meta)
	if reason != "" {
		im.note(path, "%s", reason)
		return nil
	}
	for _, key := range slices.Sorted(maps.Keys(parsed.Meta)) {
		if key != triggerKey && key != globsKey && key != globKey {
			im.note(path, noteRuleKey, key)
		}
	}
	im.unresolved(path, data)
	for _, it := range mdrules.Split([]byte(parsed.Body), path) {
		it.Description, it.Activation, it.Scope = parsed.Description, activation, slices.Clone(scope)
		if im.l.scope == adapter.ScopeProject {
			it.Targets = library.Targets{Include: []string{tool}}
		}
		im.out.Items = append(im.out.Items, it)
	}
	return nil
}

func activationOf(meta map[string]any) (library.Activation, []string, string) {
	switch meta[triggerKey] {
	case triggerAlways:
		return "", nil, ""
	case triggerModel:
		return library.ActivationAgent, nil, ""
	case triggerGlob:
		scope, ok := globList(meta)
		if !ok {
			return "", nil, noteBadGlobs
		}
		return library.ActivationFiles, scope, ""
	case triggerManual:
		return "", nil, noteManualRule
	default:
		return "", nil, noteBadTrigger
	}
}

func globList(meta map[string]any) ([]string, bool) {
	value, present := meta[globsKey]
	if !present {
		value = meta[globKey]
	}
	text, ok := value.(string)
	if !ok || strings.ContainsAny(text, "{}") {
		return nil, false
	}
	var scope []string
	for _, part := range strings.Split(text, ",") {
		if part = strings.TrimSpace(part); part != "" {
			scope = append(scope, part)
		}
	}
	return scope, len(scope) > 0
}
