package drift

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/plan"
)

const loadedTwice = "loaded twice by "

func (c checker) skillHomes(changes []plan.FileChange) []Finding {
	var findings []Finding
	for _, t := range c.targets {
		written := c.writtenHomes(t, changes)
		readers := map[string][]string{}
		reads := map[string]int{}
		for home := range written {
			for _, tool := range adapter.SkillReaders(t.Scope, home) {
				if slices.Contains(t.Selected, tool) {
					readers[home] = append(readers[home], tool)
					reads[tool]++
				}
			}
		}
		for _, home := range slices.Sorted(maps.Keys(written)) {
			twice := slices.DeleteFunc(slices.Clone(readers[home]), func(tool string) bool { return reads[tool] < 2 })
			if len(twice) > 0 {
				findings = append(findings, Finding{Kind: KindInfo, Path: written[home], Detail: loadedTwice + strings.Join(twice, ", ")})
			}
		}
	}
	return findings
}

func (c checker) writtenHomes(t adapter.Target, changes []plan.FileChange) map[string]string {
	written := map[string]string{}
	for _, tool := range t.Selected {
		a, found := c.adapter(tool)
		if !found {
			continue
		}
		for _, home := range a.Capabilities().SkillHomes[t.Scope] {
			dir := homeDir(t, home)
			if holdsUnit(changes, dir) {
				written[home] = dir
			}
		}
	}
	return written
}

func homeDir(t adapter.Target, home string) string {
	if t.Scope == adapter.ScopeProject {
		return filepath.Join(t.ProjectRoot, filepath.FromSlash(home))
	}
	return t.Env.Expand(home)
}

func holdsUnit(changes []plan.FileChange, dir string) bool {
	prefix := dir + string(filepath.Separator)
	return slices.ContainsFunc(changes, func(c plan.FileChange) bool {
		return len(c.Units) > 0 && strings.HasPrefix(c.Path, prefix)
	})
}
