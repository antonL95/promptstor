package opencode

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/secrets"
)

const (
	mcpKey         = "mcp"
	serversKey     = "servers"
	timeoutKey     = "timeout"
	enabledKey     = "enabled"
	typeKey        = "type"
	localType      = "local"
	remoteType     = "remote"
	commandKey     = "command"
	environmentKey = "environment"
	urlKey         = "url"
	headersKey     = "headers"
	cwdKey         = "cwd"
	oauthKey       = "oauth"
	transportStdio = "stdio"
	transportHTTP  = "http"

	noteServerShape        = "server %s: unexpected value types; not imported"
	noteServerType         = "server %s: has no type of local or remote, so opencode ignores it; not imported"
	noteServerDisabled     = "server %s: disabled; not imported"
	noteServerTarget       = "server %s: needs a command or a url; not imported"
	noteServerSubstitution = "server %s: uses {file:...}; not imported"
	noteServerField        = "server %s: field %s not imported"
	noteServerSecret       = "server %s: secret scrubbed, %s"
)

type mcpEntry struct {
	path   string
	fields map[string]any
}

func isServer(value any) bool {
	kind := text(object(value)[typeKey])
	return kind == localType || kind == remoteType
}

func documentServers(doc document) (entries map[string]mcpEntry, invalid []string) {
	entries = map[string]mcpEntry{}
	native := map[string]mcpEntry{}
	mcp := object(doc.fields[mcpKey])
	for _, name := range slices.Sorted(maps.Keys(mcp)) {
		value := mcp[name]
		switch {
		case name == serversKey && !isServer(value):
			for server, fields := range object(value) {
				native[server] = mcpEntry{path: doc.path, fields: object(fields)}
			}
		case name == timeoutKey && !isServer(value):
		case isServer(value):
			entries[name] = mcpEntry{path: doc.path, fields: fromLegacy(object(value))}
		default:
			invalid = append(invalid, name)
		}
	}
	maps.Copy(entries, native)
	return entries, invalid
}

func fromLegacy(fields map[string]any) map[string]any {
	out := maps.Clone(fields)
	if enabled, ok := out[enabledKey].(bool); ok {
		out[disabledKey] = !enabled
		delete(out, enabledKey)
	}
	if timeout, ok := out[timeoutKey].(json.Number); ok {
		out[timeoutKey] = map[string]any{"startup": timeout, "catalog": timeout, "execution": timeout}
	}
	return out
}

func mergedServers(documents []document) map[string]mcpEntry {
	merged := map[string]mcpEntry{}
	for _, doc := range documents {
		entries, _ := documentServers(doc)
		maps.Copy(merged, entries)
	}
	return merged
}

func (im *importer) servers(documents []document) {
	for _, doc := range documents {
		_, invalid := documentServers(doc)
		for _, name := range invalid {
			im.note(doc.path, noteServerType, name)
		}
	}
	merged := mergedServers(documents)
	for _, name := range slices.Sorted(maps.Keys(merged)) {
		im.server(name, merged[name])
	}
}

type serverValues struct {
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
}

func (im *importer) server(name string, entry mcpEntry) {
	switch {
	case entry.fields == nil:
		im.note(entry.path, noteServerShape, name)
		return
	case !isServer(entry.fields):
		im.note(entry.path, noteServerType, name)
		return
	case entry.fields[disabledKey] == true:
		im.note(entry.path, noteServerDisabled, name)
		return
	}
	var values serverValues
	encoded, err := json.Marshal(entry.fields)
	if err == nil {
		err = json.Unmarshal(encoded, &values)
	}
	if err != nil {
		im.note(entry.path, noteServerShape, name)
		return
	}
	var server library.MCPServer
	var carried []string
	switch {
	case entry.fields[typeKey] == localType && len(values.Command) > 0:
		server = library.MCPServer{Transport: transportStdio, Command: values.Command[0], Env: literals(values.Environment)}
		if len(values.Command) > 1 {
			server.Args = values.Command[1:]
		}
		carried = []string{typeKey, commandKey, environmentKey}
	case entry.fields[typeKey] == remoteType && values.URL != "":
		server = library.MCPServer{Transport: transportHTTP, URL: values.URL, Headers: literals(values.Headers)}
		carried = []string{typeKey, urlKey, headersKey}
	default:
		im.note(entry.path, noteServerTarget, name)
		return
	}
	if readsFile(server) {
		im.note(entry.path, noteServerSubstitution, name)
		return
	}
	for _, key := range slices.Sorted(maps.Keys(entry.fields)) {
		defaulted := key == disabledKey && entry.fields[key] == false
		if !defaulted && !slices.Contains(carried, key) {
			im.note(entry.path, noteServerField, name, key)
		}
	}
	scrubbed, notes := secrets.Scrub(server)
	for _, note := range notes {
		im.note(entry.path, noteServerSecret, name, note)
	}
	im.out.Items = append(im.out.Items, library.Item{Kind: library.KindMCP, Name: name, Source: entry.path, MCP: &scrubbed})
}

func literals(values map[string]string) map[string]library.Value {
	if len(values) == 0 {
		return nil
	}
	converted := make(map[string]library.Value, len(values))
	for name, value := range values {
		converted[name] = library.Value{Literal: value}
	}
	return converted
}

func readsFile(server library.MCPServer) bool {
	texts := append([]string{server.Command, server.URL}, server.Args...)
	for _, values := range []map[string]library.Value{server.Env, server.Headers} {
		for name, value := range values {
			texts = append(texts, name, value.Literal)
		}
	}
	return slices.ContainsFunc(texts, hasFileReference)
}

func hasFileReference(value string) bool { return strings.Contains(value, "{file:") }
