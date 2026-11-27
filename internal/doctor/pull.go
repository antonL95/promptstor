package doctor

import (
	"fmt"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
)

const questionPull = "Pull %s %q from %s into the library? %s does not have it."

type candidate struct {
	it     library.Item
	tool   string
	target adapter.Target
	drop   bool
	stored bool
}

func (c *candidate) kept() bool { return !c.drop }

func (d *doctor) pull(scopes []scopeImports, primary string, selected []string) ([]*candidate, error) {
	var all []*candidate
	for _, s := range scopes {
		var picked []*candidate
		for _, it := range s.byTool[primary].Items {
			if !titleOnlyRule(it) && !covered(it, picked) {
				picked = append(picked, newCandidate(it, primary, s.target))
			}
		}
		for _, tool := range selected {
			if tool == primary {
				continue
			}
			for _, it := range s.byTool[tool].Items {
				if titleOnlyRule(it) || covered(it, picked) {
					continue
				}
				ok, err := d.p.Confirm(fmt.Sprintf(questionPull, it.Kind, it.Name, tool, primary), true)
				if err != nil {
					return nil, err
				}
				if ok {
					picked = append(picked, newCandidate(it, tool, s.target))
				}
			}
		}
		all = append(all, picked...)
	}
	return all, nil
}

func newCandidate(it library.Item, tool string, t adapter.Target) *candidate {
	it.ID = library.NewID()
	return &candidate{it: it, tool: tool, target: t}
}

func covered(it library.Item, picked []*candidate) bool {
	return coveredBy(it, picked) != nil
}

func coveredBy(it library.Item, picked []*candidate) *candidate {
	for _, c := range picked {
		if c.it.Kind != it.Kind {
			continue
		}
		if normalName(c.it.Name) == normalName(it.Name) {
			return c
		}
		if it.Kind == library.KindRule && ruleText(c.it) == ruleText(it) {
			return c
		}
	}
	return nil
}

func titleOnlyRule(it library.Item) bool {
	if it.Kind != library.KindRule {
		return false
	}
	for line := range strings.Lines(it.Body) {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return false
		}
	}
	return true
}

func normalName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func ruleText(it library.Item) string {
	var lines []string
	for line := range strings.Lines(strings.ReplaceAll(it.Body, "\r\n", "\n")) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 0 {
		if heading, ok := strings.CutPrefix(lines[0], "## "); ok && normalName(heading) == normalName(it.Name) {
			lines = lines[1:]
		}
	}
	return strings.Join(lines, "\n")
}

func keptCandidates(all []*candidate) []*candidate {
	return slices.DeleteFunc(slices.Clone(all), func(c *candidate) bool { return !c.kept() })
}
