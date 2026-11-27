package codex

import (
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/plan"
)

func (Adapter) BackupSet(t adapter.Target) []plan.SnapshotEntry {
	l, ok := layoutFor(t)
	if !ok {
		return nil
	}
	names := []string{configName, hooksName, rulesDir, agentsDir, skillsDir}
	if l.scope == adapter.ScopeUser {
		names = append(names, instructionsName, promptsDir)
	}
	entries := []plan.SnapshotEntry{{Tool: tool, Path: l.sharedSkills(), Root: l.sharedRoot}}
	for _, name := range names {
		entries = append(entries, plan.SnapshotEntry{Tool: tool, Path: l.in(name), Root: l.root})
	}
	slices.SortFunc(entries, func(a, b plan.SnapshotEntry) int { return strings.Compare(a.Path, b.Path) })
	return entries
}
