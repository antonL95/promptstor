package claudecode

import (
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/plan"
)

const mcpServersPointer = "/mcpServers"

func (Adapter) BackupSet(t adapter.Target) []plan.SnapshotEntry {
	l, ok := layoutFor(t)
	if !ok {
		return nil
	}
	entry := func(path string) plan.SnapshotEntry {
		return plan.SnapshotEntry{Tool: tool, Path: path, Root: l.root}
	}
	servers := entry(l.mcp)
	if l.scope == adapter.ScopeUser {
		servers.Keys = []string{mcpServersPointer}
	}
	entries := []plan.SnapshotEntry{
		entry(l.instructions),
		entry(l.in(rulesDir)),
		entry(l.in(commandsDir)),
		entry(l.in(agentsDir)),
		entry(l.in(skillsDir)),
		entry(l.settings),
		servers,
	}
	slices.SortFunc(entries, func(a, b plan.SnapshotEntry) int { return strings.Compare(a.Path, b.Path) })
	return entries
}

func escapePointer(token string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(token)
}
