package cursor

import (
	"errors"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/mdrules"
)

const (
	noteIgnoredMarkdown = "ignored by Cursor"
	noteFrontmatter     = "frontmatter %v; not imported"
	noteManualRule      = "manual rules have no PromptStor activation; not imported"
	noteRuleKey         = "frontmatter key %s not imported"
)

var errGlobs = errors.New("globs is not the comma separated string Cursor documents")

type ruleMeta struct {
	description string
	activation  library.Activation
	scope       []string
}

func (im *importer) rules() error {
	if im.l.scope != adapter.ScopeProject {
		return nil
	}
	return im.ruleFolder(im.l.in(rulesDir))
}

func (im *importer) ruleFolder(dir string) error {
	return im.walk(dir, im.ruleFolder, func(path string, entry fs.DirEntry) error {
		switch {
		case strings.HasSuffix(entry.Name(), markdownExtension):
			im.note(path, noteIgnoredMarkdown)
			return nil
		case !strings.HasSuffix(entry.Name(), ruleExtension):
			return nil
		}
		return im.rule(path)
	})
}

func (im *importer) rule(path string) error {
	data, err := im.read(path)
	if err != nil || data == nil {
		return err
	}
	lines, body, err := splitFrontmatter(managed.StripNotice(data))
	if err != nil {
		im.note(path, noteFrontmatter, err)
		return nil
	}
	meta, ok := im.ruleMeta(path, lines)
	if !ok {
		return nil
	}
	for _, it := range mdrules.Split([]byte(body), path) {
		it.Description, it.Activation, it.Scope = meta.description, meta.activation, slices.Clone(meta.scope)
		it.Targets = library.Targets{Include: []string{tool}}
		im.out.Items = append(im.out.Items, it)
	}
	return nil
}

func (im *importer) ruleMeta(path string, lines []string) (ruleMeta, bool) {
	globLine, rest := cutGlobs(lines)
	scope, err := parseGlobs(globLine)
	if err != nil {
		im.note(path, noteFrontmatter, err)
		return ruleMeta{}, false
	}
	fields, err := decodeFields(rest)
	if err != nil {
		im.note(path, noteFrontmatter, err)
		return ruleMeta{}, false
	}
	meta := ruleMeta{}
	meta.description, _ = fields[descriptionKey].(string)
	always, _ := fields[alwaysApplyKey].(bool)
	switch {
	case always:
	case len(scope) > 0:
		meta.activation, meta.scope = library.ActivationFiles, scope
	case meta.description != "":
		meta.activation = library.ActivationAgent
	default:
		im.note(path, noteManualRule)
		return ruleMeta{}, false
	}
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		if key != descriptionKey && key != alwaysApplyKey {
			im.note(path, noteRuleKey, key)
		}
	}
	return meta, true
}

func cutGlobs(lines []string) (globs []string, rest []string) {
	inside := false
	for _, line := range lines {
		value, starts := strings.CutPrefix(line, globsKey+":")
		continues := inside && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "-"))
		switch {
		case starts:
			globs, inside = append(globs, strings.TrimSpace(value)), true
		case continues:
			globs = append(globs, strings.TrimSpace(line))
		default:
			rest, inside = append(rest, line), false
		}
	}
	return globs, rest
}

func parseGlobs(lines []string) ([]string, error) {
	lines = slices.DeleteFunc(slices.Clone(lines), func(line string) bool { return line == "" })
	if len(lines) == 0 {
		return nil, nil
	}
	value := lines[0]
	if len(lines) > 1 || strings.HasPrefix(value, "[") || strings.HasPrefix(value, "- ") || strings.ContainsAny(value, "{}") {
		return nil, errGlobs
	}
	if strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "'") {
		var unquoted string
		if yaml.Unmarshal([]byte(value), &unquoted) != nil {
			return nil, errGlobs
		}
		value = unquoted
	}
	var scope []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			scope = append(scope, part)
		}
	}
	return scope, nil
}
