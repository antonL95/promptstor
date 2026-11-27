package ci

import (
	"maps"
	"slices"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/secrets"
)

type literal struct {
	id        library.ID
	item, key string
}

type serverField struct {
	key  string
	only library.MCPServer
}

func literalSecrets(items []library.Item) []literal {
	var found []literal
	for _, it := range items {
		if it.Kind == library.KindHook && secrets.HoldsLiteral(it.Native) {
			found = append(found, literal{id: it.ID, item: it.Name, key: hookKey})
		}
		if it.MCP == nil {
			continue
		}
		for _, field := range serverFields(*it.MCP) {
			if _, notes := secrets.Scrub(field.only); len(notes) > 0 {
				found = append(found, literal{id: it.ID, item: it.Name, key: field.key})
			}
		}
	}
	return found
}

const hookKey = "hook"

func serverFields(s library.MCPServer) []serverField {
	fields := []serverField{
		{"command", library.MCPServer{Command: s.Command}},
		{"args", library.MCPServer{Args: s.Args}},
		{"url", library.MCPServer{URL: s.URL}},
	}
	for _, name := range slices.Sorted(maps.Keys(s.Env)) {
		fields = append(fields, serverField{"env/" + name, library.MCPServer{Env: map[string]library.Value{name: s.Env[name]}}})
	}
	for _, name := range slices.Sorted(maps.Keys(s.Headers)) {
		fields = append(fields, serverField{"headers/" + name, library.MCPServer{Headers: map[string]library.Value{name: s.Headers[name]}}})
	}
	return fields
}

func hide(units []plan.Unit, literals []literal) {
	for i := range units {
		if slices.ContainsFunc(literals, func(l literal) bool { return slices.Contains(units[i].Items, l.id) }) {
			units[i].Secret = true
		}
	}
}
