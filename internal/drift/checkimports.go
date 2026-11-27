package drift

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/manifest"
)

const notImported = "not imported"

func (c checker) imports() ([]Finding, error) {
	var findings []Finding
	for _, t := range c.targets {
		for _, tool := range t.Selected {
			a, found := c.adapter(tool)
			if !found {
				return nil, fmt.Errorf("unknown tool %q", tool)
			}
			imported, err := a.Import(t)
			if err != nil {
				return nil, fmt.Errorf("import %s for %s scope: %w", tool, t.Scope, err)
			}
			findings = append(findings, c.importFindings(tool, imported)...)
		}
	}
	return findings, nil
}

func (c checker) importFindings(tool string, imported adapter.Imported) []Finding {
	var findings []Finding
	for _, g := range imported.Grants {
		if c.written(g.Path, g.Key) {
			continue
		}
		findings = append(findings, Finding{Kind: KindRuntimeGrant, Tool: cmp.Or(g.Tool, tool), Name: g.Value, Path: g.Path, Detail: g.Key})
	}
	for _, f := range imported.Foreign {
		findings = append(findings, Finding{Kind: KindForeign, Tool: tool, Name: f.Owner, Path: f.Path, Detail: f.Reason})
	}
	for _, n := range imported.Notes {
		kind := KindInfo
		if strings.Contains(n.Message, notImported) {
			kind = KindIgnoredFile
		}
		findings = append(findings, Finding{Kind: kind, Tool: tool, Path: n.Path, Detail: n.Message})
	}
	for _, u := range imported.Unresolved {
		findings = append(findings, unresolvedFinding(tool, u))
	}
	return findings
}

func (c checker) written(path, key string) bool {
	return slices.ContainsFunc(c.manifest.Entries, func(e manifest.Entry) bool {
		return e.Path == path && e.Key == key
	})
}
