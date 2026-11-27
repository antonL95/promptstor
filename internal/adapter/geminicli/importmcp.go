package geminicli

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/secrets"
)

const (
	serversKey     = "mcpServers"
	transportStdio = "stdio"
	transportHTTP  = "http"
	transportSSE   = "sse"
)

var serverFields = []string{"command", "args", "env", "url", "type", "headers", "httpUrl"}

type serverDoc struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Type    string            `json:"type,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	HTTPURL string            `json:"httpUrl,omitempty"`
}

func (im *importer) servers(servers map[string]any) {
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		im.server(name, object(servers, name))
	}
}

func (im *importer) server(name string, fields map[string]any) {
	path := im.l.settings
	var doc serverDoc
	encoded, err := json.Marshal(fields)
	if err == nil {
		err = json.Unmarshal(encoded, &doc)
	}
	if err != nil {
		im.note(path, "server %s: unexpected value types; not imported", name)
		return
	}
	server, err := libraryServer(doc)
	if err != nil {
		im.note(path, "server %s: %v; not imported", name, err)
		return
	}
	for _, field := range slices.Sorted(maps.Keys(fields)) {
		if !slices.Contains(serverFields, field) {
			im.note(path, "server %s: field %s not imported", name, field)
		}
	}
	scrubbed, notes := secrets.Scrub(server)
	for _, note := range notes {
		im.note(path, "server %s: secret scrubbed, %s", name, note)
	}
	im.out.Items = append(im.out.Items, library.Item{Kind: library.KindMCP, Name: name, Source: path, MCP: &scrubbed})
}

func libraryServer(doc serverDoc) (library.MCPServer, error) {
	server := library.MCPServer{Command: doc.Command, Args: doc.Args, Env: values(doc.Env), Headers: values(doc.Headers)}
	switch {
	case doc.HTTPURL != "":
		server.Transport, server.URL, server.Command = transportHTTP, doc.HTTPURL, ""
	case doc.URL != "" && doc.Type == transportSSE:
		server.Transport, server.URL, server.Command = transportSSE, doc.URL, ""
	case doc.URL != "" && (doc.Type == "" || doc.Type == transportHTTP):
		server.Transport, server.URL, server.Command = transportHTTP, doc.URL, ""
	case doc.URL == "" && doc.Command != "":
		server.Transport = transportStdio
	default:
		return server, errors.New("needs a command, a url or an httpUrl with a known type")
	}
	return server, nil
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
