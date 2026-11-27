package antigravity

import (
	"encoding/json"
	"maps"
	"slices"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/secrets"
)

const (
	noteServerShape    = "server %s: unexpected value types; not imported"
	noteServerHTTPURL  = "server %s: httpUrl is not supported by Antigravity; not imported"
	noteServerDisabled = "server %s: disabled; not imported"
	noteServerTarget   = "server %s: needs a command or a serverUrl; not imported"
	noteServerField    = "server %s: field %s not imported"
	noteServerSecret   = "server %s: secret scrubbed, %s"
	httpURLField       = "httpUrl"
	disabledField      = "disabled"
)

func (im *importer) servers() error {
	path := im.l.in(mcpName)
	config, err := im.object(path)
	if err != nil {
		return err
	}
	servers := rawObject(config[serversKey])
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		im.server(path, name, servers[name])
	}
	return nil
}

func (im *importer) server(path, name string, raw json.RawMessage) {
	fields := rawObject(raw)
	var doc serverDoc
	if fields == nil || json.Unmarshal(raw, &doc) != nil {
		im.note(path, noteServerShape, name)
		return
	}
	switch {
	case fields[httpURLField] != nil:
		im.note(path, noteServerHTTPURL, name)
		return
	case string(doc.Disabled) == "true":
		im.note(path, noteServerDisabled, name)
		return
	}
	server := library.MCPServer{URL: doc.ServerURL}
	if server.URL == "" {
		server.URL = doc.URL
	}
	var carried []string
	switch {
	case server.URL != "":
		server.Transport, server.Headers = transportHTTP, values(doc.Headers)
		carried = []string{"serverUrl", "url", "headers"}
	case doc.Command != "":
		server.Transport, server.Command, server.Args, server.Env = transportStdio, doc.Command, doc.Args, values(doc.Env)
		carried = []string{"command", "args", "env"}
	default:
		im.note(path, noteServerTarget, name)
		return
	}
	for _, field := range slices.Sorted(maps.Keys(fields)) {
		defaulted := field == disabledField && string(fields[field]) == "false"
		if !defaulted && !slices.Contains(carried, field) {
			im.note(path, noteServerField, name, field)
		}
	}
	scrubbed, notes := secrets.Scrub(server)
	for _, note := range notes {
		im.note(path, noteServerSecret, name, note)
	}
	im.out.Items = append(im.out.Items, library.Item{Kind: library.KindMCP, Name: name, Source: path, MCP: &scrubbed})
}

func values(plain map[string]string) map[string]library.Value {
	if len(plain) == 0 {
		return nil
	}
	converted := make(map[string]library.Value, len(plain))
	for name, value := range plain {
		converted[name] = library.Value{Literal: value}
	}
	return converted
}
