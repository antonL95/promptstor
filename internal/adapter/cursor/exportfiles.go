package cursor

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
)

const (
	nameKey               = "name"
	reasonSkillName       = "Cursor requires skill names of lowercase letters, digits and hyphens"
	reasonSkillSummary    = "Cursor requires a skill description"
	reasonNoPrompt        = "the command has no prompt"
	reasonCommandFields   = "written without %s, because Cursor command frontmatter is not documented"
	reasonToolRestriction = "Cursor subagents have no tools field, so %s does not restrict this one"
	reasonDropped         = "written without %s, which Cursor agent files do not take"
	reasonCLIAgents       = "the Cursor CLI ignores ~/.cursor/agents"
)

var toolRestrictions = []string{"tools", "disallowedTools"}

type agentField struct {
	meta, name string
	accepts    func(any) bool
}

var agentFields = []agentField{
	{"cursor_model", "model", isText},
	{"cursor_readonly", "readonly", isBool},
	{"cursor_is_background", "is_background", isBool},
}

func isText(value any) bool {
	_, ok := value.(string)
	return ok
}

func isBool(value any) bool {
	_, ok := value.(bool)
	return ok
}

func (ex *exporter) skill(it library.Item) {
	units, err := adapter.SkillUnits(ex.t, tool, it)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	home := filepath.Join(ex.l.sharedSkills(), it.Name)
	for _, u := range units {
		if backup.Excluded(ex.t.Env, u.Path) {
			relative, _ := filepath.Rel(home, u.Path)
			ex.report(it, fmt.Sprintf(reasonCredentialPath, "skill file "+filepath.ToSlash(relative)))
			return
		}
	}
	switch {
	case library.Slug(it.Name) != it.Name:
		ex.report(it, reasonSkillName)
	case strings.TrimSpace(it.Description) == "":
		ex.report(it, reasonSkillSummary)
	default:
		ex.units = append(ex.units, units...)
	}
}

func (ex *exporter) command(it library.Item) {
	slug := library.Slug(it.Name)
	switch {
	case slug == "":
		ex.report(it, reasonNoFileName)
		return
	case strings.TrimSpace(it.Body) == "":
		ex.report(it, reasonNoPrompt)
		return
	case !utf8.ValidString(it.Body):
		ex.report(it, reasonText)
		return
	}
	if !ex.document(it, ex.l.in(commandsDir, slug+markdownExtension), managed.WithNotice([]byte(it.Body))) {
		return
	}
	dropped := slices.DeleteFunc(it.MetaKeys(), func(key string) bool { return key == nameKey || key == descriptionKey })
	if it.Description != "" {
		dropped = append(dropped, descriptionKey)
	}
	ex.reportDropped(it, dropped, reasonCommandFields)
}

func (ex *exporter) subagent(it library.Item) {
	slug := library.Slug(it.Name)
	switch {
	case slug == "":
		ex.report(it, reasonNoFileName)
		return
	case !utf8.ValidString(it.Description + it.Body):
		ex.report(it, reasonText)
		return
	}
	front := newHeader()
	front.set(nameKey, slug)
	if it.Description != "" {
		front.set(descriptionKey, it.Description)
	}
	written := []string{nameKey, descriptionKey}
	for _, field := range agentFields {
		if value, present := it.Meta[field.meta]; present && field.accepts(value) {
			front.set(field.name, value)
			written = append(written, field.meta)
		}
	}
	content, err := front.markdown(it.Body)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	if !ex.document(it, ex.l.in(agentsDir, slug+markdownExtension), content) {
		return
	}
	var dropped []string
	for _, key := range slices.Sorted(slices.Values(it.MetaKeys())) {
		switch {
		case slices.Contains(toolRestrictions, key):
			ex.report(it, fmt.Sprintf(reasonToolRestriction, key))
		case !slices.Contains(written, key):
			dropped = append(dropped, key)
		}
	}
	ex.reportDropped(it, dropped, reasonDropped)
	if ex.l.scope == adapter.ScopeUser {
		ex.report(it, reasonCLIAgents)
	}
}

func (ex *exporter) reportDropped(it library.Item, keys []string, reason string) {
	if len(keys) == 0 {
		return
	}
	slices.Sort(keys)
	ex.report(it, fmt.Sprintf(reason, strings.Join(keys, ", ")))
}
