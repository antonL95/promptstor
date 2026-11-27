package cursor

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/secrets"
)

const (
	noteServerShape    = "server %s: unexpected value types; not imported"
	noteServerTarget   = "server %s: needs a command or a url; not imported"
	noteServerVariable = "server %s: uses %s, which PromptStor cannot carry; not imported"
	noteServerField    = "server %s: field %s not imported"
	noteServerSecret   = "server %s: secret scrubbed, %s"
)

var cursorVariable = regexp.MustCompile(`\$\{(userHome|workspaceFolder|workspaceFolderBasename|pathSeparator|/)\}`)

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
	var server library.MCPServer
	var carried []string
	switch {
	case doc.URL != "":
		server = library.MCPServer{Transport: transportHTTP, URL: doc.URL, Headers: values(doc.Headers)}
		carried = []string{typeField, urlField, headersField}
	case doc.Command != "":
		server = library.MCPServer{Transport: transportStdio, Command: doc.Command, Args: doc.Args, Env: values(doc.Env)}
		carried = []string{typeField, commandField, argsField, envField}
	default:
		im.note(path, noteServerTarget, name)
		return
	}
	if found := findCursorVariable(server); found != "" {
		im.note(path, noteServerVariable, name, found)
		return
	}
	for _, field := range slices.Sorted(maps.Keys(fields)) {
		if !slices.Contains(carried, field) {
			im.note(path, noteServerField, name, field)
		}
	}
	scrubbed, notes := secrets.Scrub(server)
	for _, note := range notes {
		im.note(path, noteServerSecret, name, note)
	}
	im.out.Items = append(im.out.Items, library.Item{Kind: library.KindMCP, Name: name, Source: path, MCP: &scrubbed})
}

func findCursorVariable(server library.MCPServer) string {
	texts := append([]string{server.Command, server.URL}, server.Args...)
	for _, section := range []map[string]library.Value{server.Env, server.Headers} {
		for _, name := range slices.Sorted(maps.Keys(section)) {
			texts = append(texts, section[name].Literal)
		}
	}
	for _, text := range texts {
		if found := cursorVariable.FindString(text); found != "" {
			return found
		}
	}
	return ""
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
