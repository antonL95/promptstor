package claudecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/secrets"
)

const (
	transportStdio = "stdio"
	transportHTTP  = "http"
	transportSSE   = "sse"
	streamableHTTP = "streamable-http"
	boostServer    = "laravel-boost"
	boostMCPReason = "mcpServers/" + boostServer + "; Boost regenerates it"
)

var serverFields = []string{"type", "command", "args", "env", "url", "headers"}

type serverDoc struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type serversDoc struct {
	Servers  map[string]json.RawMessage `json:"mcpServers"`
	Projects map[string]struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	} `json:"projects"`
}

func (im *importer) servers() error {
	path := im.l.mcp
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var doc serversDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		im.note(path, "mcpServers: %v; not imported", err)
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(doc.Servers)) {
		if im.l.scope == adapter.ScopeProject && name == boostServer {
			im.foreign(path, boostOwner, boostMCPReason)
			continue
		}
		im.server(path, name, doc.Servers[name])
	}
	if im.l.scope == adapter.ScopeUser {
		for _, project := range slices.Sorted(maps.Keys(doc.Projects)) {
			for _, name := range slices.Sorted(maps.Keys(doc.Projects[project].Servers)) {
				im.note(path, "local scope server %s for %s; not imported", name, project)
			}
		}
	}
	return nil
}

func (im *importer) server(path, name string, raw json.RawMessage) {
	var fields map[string]json.RawMessage
	var server serverDoc
	if err := json.Unmarshal(raw, &fields); err != nil {
		im.note(path, "server %s: %v; not imported", name, err)
		return
	}
	if err := json.Unmarshal(raw, &server); err != nil {
		im.note(path, "server %s: %v; not imported", name, err)
		return
	}
	transport, reason := transportOf(server)
	if reason != "" {
		im.note(path, "server %s: %s; not imported", name, reason)
		return
	}
	for _, field := range slices.Sorted(maps.Keys(fields)) {
		if !slices.Contains(serverFields, field) {
			im.note(path, "server %s: field %s not imported", name, field)
		}
	}
	scrubbed, notes := secrets.Scrub(library.MCPServer{
		Transport: transport,
		Command:   server.Command,
		Args:      server.Args,
		URL:       server.URL,
		Env:       values(server.Env),
		Headers:   values(server.Headers),
	})
	for _, note := range notes {
		im.note(path, "server %s: secret scrubbed, %s", name, note)
	}
	im.out.Items = append(im.out.Items, library.Item{Kind: library.KindMCP, Name: name, Source: path, MCP: &scrubbed})
}

func transportOf(server serverDoc) (string, string) {
	switch server.Type {
	case "":
		if server.URL != "" {
			return "", "url without type, which Claude Code rejects"
		}
		return transportStdio, ""
	case transportStdio, transportHTTP, transportSSE:
		return server.Type, ""
	case streamableHTTP:
		return transportHTTP, ""
	default:
		return "", "transport " + server.Type + " is not supported"
	}
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
