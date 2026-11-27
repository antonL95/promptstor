package claudecode

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/secrets"
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
	entries := []plan.SnapshotEntry{
		entry(l.instructions),
		entry(l.in(rulesDir)),
		entry(l.in(commandsDir)),
		entry(l.in(agentsDir)),
		entry(l.in(skillsDir)),
	}
	settings := entry(l.settings)
	settings.RedactKeys = settingsRedactions(l.settings)
	servers := entry(l.mcp)
	servers.RedactKeys = serverRedactions(l.mcp)
	if l.scope == adapter.ScopeUser {
		servers.Keys = []string{mcpServersPointer}
	}
	entries = append(entries, settings, servers)
	slices.SortFunc(entries, func(a, b plan.SnapshotEntry) int { return strings.Compare(a.Path, b.Path) })
	return entries
}

func settingsRedactions(path string) []string {
	var doc struct {
		Env map[string]json.RawMessage `json:"env"`
	}
	if !readJSON(path, &doc) {
		return nil
	}
	return secretPointers("/env", doc.Env)
}

func serverRedactions(path string) []string {
	var doc struct {
		Servers map[string]struct {
			Env     map[string]json.RawMessage `json:"env"`
			Headers map[string]json.RawMessage `json:"headers"`
		} `json:"mcpServers"`
	}
	if !readJSON(path, &doc) {
		return nil
	}
	var keys []string
	for _, name := range slices.Sorted(maps.Keys(doc.Servers)) {
		server := doc.Servers[name]
		prefix := mcpServersPointer + "/" + escapePointer(name)
		keys = append(keys, secretPointers(prefix+"/env", server.Env)...)
		keys = append(keys, secretPointers(prefix+"/headers", server.Headers)...)
	}
	return keys
}

func secretPointers(prefix string, values map[string]json.RawMessage) []string {
	var keys []string
	for _, name := range slices.Sorted(maps.Keys(values)) {
		if secrets.IsSecretName(name) {
			keys = append(keys, prefix+"/"+escapePointer(name))
		}
	}
	return keys
}

func readJSON(path string, into any) bool {
	data, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(data, into) == nil
}

func escapePointer(token string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(token)
}
