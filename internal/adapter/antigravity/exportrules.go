package antigravity

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/adapter/agentsmd"
	"github.com/antonL95/promptstor/internal/adapter/geminicli"
	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/library"
)

const (
	triggerKey        = "trigger"
	descriptionKey    = "description"
	globsKey          = "globs"
	globKey           = "glob"
	triggerAlways     = "always_on"
	triggerGlob       = "glob"
	triggerModel      = "model_decision"
	triggerManual     = "manual"
	globSeparator     = ", "
	anyDirectories    = "**/"
	maxRuleBytes      = 24000
	ruleTokenBudget   = 20000
	bytesPerToken     = 4
	reasonActivation  = "activation %q has no Antigravity trigger"
	reasonNoGlobs     = "Antigravity needs globs for a glob rule"
	reasonNoSummary   = "Antigravity needs a description for a model_decision rule"
	reasonGlob        = "glob %q cannot be written; Antigravity takes comma separated globs without braces"
	reasonLinkedShare = "relies on Gemini CLI's memory file, which is behind a symlink and cannot be written"
	reasonRuleSize    = "the rule file is %d bytes; Antigravity truncates it after %d"
	reasonBudget      = "always-on rules come to about %d tokens; Antigravity demotes the largest above %d"
)

type alwaysTotal struct {
	size, largestSize int
	largest           library.Item
}

func (a *alwaysTotal) count(it library.Item, size int) {
	a.size += size
	if size > a.largestSize {
		a.largest, a.largestSize = it, size
	}
}

func (ex *exporter) rules(items []library.Item) {
	var total alwaysTotal
	var own []library.Item
	if ex.l.scope == adapter.ScopeProject {
		shared, perTool := agentsmd.Partition(items, ex.t.Selected)
		own = perTool[tool]
		for _, it := range shared {
			if targets(it, tool) && isAlways(it) {
				total.count(it, len(it.Body))
			}
		}
	} else {
		for _, it := range items {
			if it.Kind == library.KindRule {
				own = append(own, it)
			}
		}
	}
	used := map[string]int{}
	for _, it := range own {
		ex.rule(it, used, &total)
	}
	if tokens := total.size / bytesPerToken; tokens > ruleTokenBudget {
		ex.report(total.largest, fmt.Sprintf(reasonBudget, tokens, ruleTokenBudget))
	}
}

func targets(it library.Item, id adapter.ToolID) bool {
	merged := library.Merged{Items: []library.Item{it}}
	return len(merged.ForTool(id)) == 1
}

func isAlways(it library.Item) bool {
	return (it.Activation == "" || it.Activation == library.ActivationAlways) && len(it.Scope) == 0
}

func (ex *exporter) rule(it library.Item, used map[string]int, total *alwaysTotal) {
	slug := library.Slug(it.Name)
	if slug == "" {
		ex.report(it, reasonNoFileName)
		return
	}
	frontmatter, trigger, reason := ruleHeader(it)
	if reason != "" {
		ex.report(it, reason)
		return
	}
	content, err := frontmatter.markdown(it.Body)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	if trigger == triggerAlways {
		total.count(it, len(content))
	}
	if relied, linked := ex.reachesThroughGemini(it); relied {
		if linked {
			ex.report(it, reasonLinkedShare)
		}
		return
	}
	if used[slug]++; used[slug] > 1 {
		slug = fmt.Sprintf("%s-%d", slug, used[slug])
	}
	if !ex.document(it, ex.l.in(rulesDir, slug+markdownExtension), content) {
		return
	}
	if len(content) > maxRuleBytes {
		ex.report(it, fmt.Sprintf(reasonRuleSize, len(content), maxRuleBytes))
	}
}

func ruleHeader(it library.Item) (*header, string, string) {
	var trigger string
	switch it.Activation {
	case "", library.ActivationAlways:
		trigger = triggerAlways
		if len(it.Scope) > 0 {
			trigger = triggerGlob
		}
	case library.ActivationFiles:
		trigger = triggerGlob
	case library.ActivationAgent:
		trigger = triggerModel
	default:
		return nil, "", fmt.Sprintf(reasonActivation, it.Activation)
	}
	var globs []string
	if trigger == triggerGlob {
		if len(it.Scope) == 0 {
			return nil, "", reasonNoGlobs
		}
		for _, pattern := range it.Scope {
			if strings.TrimSpace(pattern) == "" || strings.ContainsAny(pattern, ",{}\n") {
				return nil, "", fmt.Sprintf(reasonGlob, pattern)
			}
			globs = append(globs, nativeGlob(pattern))
		}
	}
	if trigger == triggerModel && strings.TrimSpace(it.Description) == "" {
		return nil, "", reasonNoSummary
	}
	frontmatter := newHeader()
	frontmatter.set(triggerKey, trigger)
	if it.Description != "" {
		frontmatter.set(descriptionKey, it.Description)
	}
	if len(globs) > 0 {
		frontmatter.set(globsKey, strings.Join(globs, globSeparator))
	}
	return frontmatter, trigger, ""
}

func nativeGlob(pattern string) string {
	if !strings.Contains(pattern, "/") || strings.HasPrefix(pattern, "/") || strings.HasPrefix(pattern, anyDirectories) {
		return pattern
	}
	return anyDirectories + pattern
}

func (ex *exporter) reachesThroughGemini(it library.Item) (relied, linked bool) {
	if ex.l.scope != adapter.ScopeUser || !isAlways(it) ||
		!slices.Contains(ex.t.Selected, adapter.GeminiCLI) || !targets(it, adapter.GeminiCLI) {
		return false, false
	}
	memory, reason := geminicli.UserMemoryFile(ex.t.Env)
	if reason != "" || !slices.Contains([]string{memoryName, agentsName}, filepath.Base(memory)) {
		return false, false
	}
	_, linked, err := fsx.LinkedAncestor(ex.l.root, memory)
	return true, linked || err != nil
}
