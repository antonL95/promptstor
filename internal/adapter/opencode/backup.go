package opencode

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
	entries := []plan.SnapshotEntry{{Tool: tool, Path: l.sharedSkills(), Root: l.sharedRoot}}
	var set []string
	switch {
	case l.unknownConfig:
	case l.scope == adapter.ScopeProject:
		set = []string{l.at(jsonName), l.at(jsoncName), l.content}
	default:
		set = []string{l.at(instructionsName), l.at(jsonName), l.at(jsoncName)}
		for _, dir := range []string{legacyAgentsDir, agentsDir, legacyModesDir, modesDir, legacyCommandsDir, commandsDir} {
			set = append(set, l.in(dir))
		}
	}
	for _, path := range set {
		entries = append(entries, plan.SnapshotEntry{Tool: tool, Path: path, Root: l.root})
	}
	slices.SortFunc(entries, func(a, b plan.SnapshotEntry) int { return strings.Compare(a.Path, b.Path) })
	return entries
}
