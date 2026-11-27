package cursor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/adapter/agentsmd"
	"github.com/antonL95/promptstor/internal/library"
)

const (
	reasonUserRules    = "Cursor user rules live in your Cursor account"
	reasonClaudeLeak   = "Cursor also reads CLAUDE.md"
	reasonActivation   = "activation %q has no Cursor rule type"
	reasonNoGlobs      = "Cursor needs globs for a file rule"
	reasonNoSummary    = "Cursor needs a description for an agent rule"
	reasonGlob         = "glob %q cannot be written in Cursor's comma separated globs"
	reasonAgentGlobs   = "scope globs are not written on an agent rule"
	descriptionKey     = "description"
	globsKey           = "globs"
	alwaysApplyKey     = "alwaysApply"
	globSeparator      = ", "
	globForbidden      = ",{}\n\r"
	globForbiddenStart = "\"'[!#&|>%@`"
)

func (ex *exporter) rules(items []library.Item) {
	if ex.l.scope == adapter.ScopeUser {
		for _, it := range items {
			if it.Kind == library.KindRule {
				ex.report(it, reasonUserRules)
			}
		}
		return
	}
	_, perTool := agentsmd.Partition(items, ex.t.Selected)
	used := map[string]int{}
	for _, it := range perTool[tool] {
		ex.rule(it, used)
	}
	if slices.Contains(ex.t.Selected, adapter.ClaudeCode) {
		ex.claudeOnlyRules(perTool[adapter.ClaudeCode], perTool[tool])
	}
}

func (ex *exporter) claudeOnlyRules(claude, own []library.Item) {
	for _, it := range claude {
		if !slices.ContainsFunc(own, func(mine library.Item) bool { return mine.ID == it.ID && mine.Name == it.Name }) {
			ex.report(it, reasonClaudeLeak)
		}
	}
}

func (ex *exporter) rule(it library.Item, used map[string]int) {
	slug := library.Slug(it.Name)
	switch {
	case slug == "":
		ex.report(it, reasonNoFileName)
		return
	case !utf8.ValidString(it.Description + it.Body):
		ex.report(it, reasonText)
		return
	}
	activation := it.Activation
	if (activation == "" || activation == library.ActivationAlways) && len(it.Scope) > 0 {
		activation = library.ActivationFiles
	}
	description := strings.Join(strings.Fields(it.Description), " ")
	front := newHeader()
	if description != "" {
		front.set(descriptionKey, description)
	}
	globs, always := "", false
	switch activation {
	case "", library.ActivationAlways:
		always = true
	case library.ActivationFiles:
		if len(it.Scope) == 0 {
			ex.report(it, reasonNoGlobs)
			return
		}
		if at := slices.IndexFunc(it.Scope, unwritableGlob); at >= 0 {
			ex.report(it, fmt.Sprintf(reasonGlob, it.Scope[at]))
			return
		}
		globs = strings.Join(it.Scope, globSeparator)
	case library.ActivationAgent:
		if description == "" {
			ex.report(it, reasonNoSummary)
			return
		}
	default:
		ex.report(it, fmt.Sprintf(reasonActivation, it.Activation))
		return
	}
	if globs != "" {
		front.setUnquoted(globsKey, globs)
	}
	front.setUnquoted(alwaysApplyKey, strconv.FormatBool(always))
	content, err := front.markdown(it.Body)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	if used[slug]++; used[slug] > 1 {
		slug = fmt.Sprintf("%s-%d", slug, used[slug])
	}
	if ex.document(it, ex.l.in(rulesDir, slug+ruleExtension), content) && activation == library.ActivationAgent && len(it.Scope) > 0 {
		ex.report(it, reasonAgentGlobs)
	}
}

func unwritableGlob(pattern string) bool {
	return pattern == "" || pattern != strings.TrimSpace(pattern) || !utf8.ValidString(pattern) ||
		strings.ContainsAny(pattern, globForbidden) || strings.ContainsAny(pattern[:1], globForbiddenStart) ||
		strings.Contains(pattern, " #") || strings.Contains(pattern, ": ")
}
