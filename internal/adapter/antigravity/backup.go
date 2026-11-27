package antigravity

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
	set := []string{l.in(rulesDir), l.in(skillsDir), l.in(agentsDir), l.in(mcpName)}
	if l.scope == adapter.ScopeUser {
		set = append(set, l.in(hooksName), l.cliSettings())
	}
	entries := make([]plan.SnapshotEntry, 0, len(set))
	for _, path := range set {
		entries = append(entries, plan.SnapshotEntry{Tool: tool, Path: path, Root: l.root})
	}
	slices.SortFunc(entries, func(a, b plan.SnapshotEntry) int { return strings.Compare(a.Path, b.Path) })
	return entries
}
