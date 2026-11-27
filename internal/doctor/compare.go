package doctor

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/fsx"
)

type scopeImports struct {
	target  adapter.Target
	byTool  map[string]adapter.Imported
	ordered []string
}

func (d *doctor) importAll(tools []string) ([]scopeImports, error) {
	var scopes []scopeImports
	for _, t := range d.targets(tools) {
		s := scopeImports{target: t, byTool: map[string]adapter.Imported{}}
		for _, tool := range tools {
			a, ok := d.adapter(tool)
			if !ok {
				return nil, fmt.Errorf("unknown tool %q", tool)
			}
			imported, err := a.Import(t)
			if err != nil {
				return nil, fmt.Errorf("import %s for %s scope: %w", tool, t.Scope, err)
			}
			s.byTool[tool] = imported
			s.ordered = append(s.ordered, tool)
		}
		scopes = append(scopes, s)
	}
	return scopes, nil
}

func itemCounts(scopes []scopeImports, own map[string][]string) map[string]int {
	counts := map[string]int{}
	for _, s := range scopes {
		for tool, imported := range s.byTool {
			for _, it := range imported.Items {
				if _, mine := fsx.Within(own[tool], it.Source); mine && it.Source != "" && !titleOnlyRule(it) {
					counts[tool]++
				}
			}
		}
	}
	return counts
}

func (d *doctor) printComparison(scopes []scopeImports) []drift.Finding {
	var all []drift.Finding
	plugins := false
	for _, s := range scopes {
		findings := drift.CompareTools(s.byTool)
		for _, tool := range s.ordered {
			for _, f := range s.byTool[tool].Foreign {
				findings = append(findings, drift.Finding{Kind: drift.KindForeign, Tool: tool, Name: f.Owner, Path: f.Path, Detail: f.Reason})
				plugins = plugins || pluginForeign(tool, f.Owner)
			}
		}
		d.w.printf("\nHow your tools differ (%s scope):\n", s.target.Scope)
		if len(findings) == 0 {
			d.w.printf("  they agree\n")
		}
		slices.SortStableFunc(findings, func(a, b drift.Finding) int {
			return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Tool, b.Tool), cmp.Compare(a.Path, b.Path), cmp.Compare(a.Name, b.Name))
		})
		for _, f := range findings {
			d.w.printf("  %s  %s  %s  %s\n", f.Kind, f.Tool, cmp.Or(f.Name, f.Path), cmp.Or(f.Detail, f.Path))
		}
		all = append(all, findings...)
	}
	if plugins {
		d.w.printf("\nNote: %s.\n", noteAntigravity)
	}
	return all
}
