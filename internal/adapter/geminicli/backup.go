package geminicli

import (
	"path/filepath"
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
	set := []string{l.settings, l.in(commandsDir), l.in(agentsDir)}
	if l.scope == adapter.ScopeUser {
		memory, reason := l.memoryFile(t.Env)
		if reason != "" {
			memory = l.in(memoryName)
		}
		set = append(set, memory, l.in(policiesDir), l.sharedSkills())
	} else {
		set = append(set, filepath.Join(l.root, memoryName))
	}
	entries := make([]plan.SnapshotEntry, 0, len(set))
	for _, path := range set {
		entries = append(entries, plan.SnapshotEntry{Tool: tool, Path: path, Root: l.root})
	}
	slices.SortFunc(entries, func(a, b plan.SnapshotEntry) int { return strings.Compare(a.Path, b.Path) })
	return entries
}
