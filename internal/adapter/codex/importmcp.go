package codex

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/secrets"
)

const (
	serversKey     = "mcp_servers"
	transportStdio = "stdio"
	transportHTTP  = "http"

	fieldCommand        = "command"
	fieldArgs           = "args"
	fieldURL            = "url"
	fieldEnv            = "env"
	fieldEnvVars        = "env_vars"
	fieldBearer         = "bearer_token_env_var"
	fieldHeaders        = "http_headers"
	fieldEnvHeaders     = "env_http_headers"
	fieldEnabled        = "enabled"
	authorizationHeader = "Authorization"
	bearerPrefix        = "Bearer "
	envScheme           = "env:"
	localSource         = "local"
)

var serverFields = []string{
	fieldCommand, fieldArgs, fieldURL, fieldEnv, fieldEnvVars,
	fieldBearer, fieldHeaders, fieldEnvHeaders, fieldEnabled,
}

type serverDoc struct {
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	URL        string            `json:"url"`
	Env        map[string]string `json:"env"`
	EnvVars    []any             `json:"env_vars"`
	Bearer     string            `json:"bearer_token_env_var"`
	Headers    map[string]string `json:"http_headers"`
	EnvHeaders map[string]string `json:"env_http_headers"`
	Enabled    *bool             `json:"enabled"`
}

func (im *importer) servers(path string, servers map[string]any) {
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		im.server(path, name, subtable(servers, name))
	}
}

func (im *importer) server(path, name string, fields map[string]any) {
	var doc serverDoc
	encoded, err := json.Marshal(fields)
	if err == nil {
		err = json.Unmarshal(encoded, &doc)
	}
	if err != nil {
		im.note(path, "server %s: unexpected value types; not imported", name)
		return
	}
	if doc.Enabled != nil && !*doc.Enabled {
		im.note(path, "server %s is disabled; not imported", name)
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
	server := library.MCPServer{Command: doc.Command, Args: doc.Args, URL: doc.URL}
	switch {
	case doc.Command != "" && doc.URL == "":
		server.Transport = transportStdio
	case doc.URL != "" && doc.Command == "":
		server.Transport = transportHTTP
	default:
		return server, errors.New("needs either command or url")
	}
	env := map[string]library.Value{}
	for name, value := range doc.Env {
		env[name] = library.Value{Literal: value}
	}
	for _, entry := range doc.EnvVars {
		name, err := localVariable(entry)
		if err != nil {
			return server, err
		}
		env[name] = library.Value{Ref: envScheme + name}
	}
	headers := map[string]library.Value{}
	for name, value := range doc.Headers {
		headers[name] = library.Value{Literal: value}
	}
	for name, variable := range doc.EnvHeaders {
		headers[name] = library.Value{Ref: envScheme + variable}
	}
	if doc.Bearer != "" {
		headers[authorizationHeader] = library.Value{Literal: bearerPrefix + envRef(doc.Bearer)}
	}
	if len(env) > 0 {
		server.Env = env
	}
	if len(headers) > 0 {
		server.Headers = headers
	}
	return server, nil
}

func localVariable(entry any) (string, error) {
	switch typed := entry.(type) {
	case string:
		return typed, nil
	case map[string]any:
		name, _ := typed["name"].(string)
		source, _ := typed["source"].(string)
		if name != "" && (source == "" || source == localSource) {
			return name, nil
		}
	}
	return "", errors.New("env_vars holds an entry that is not a local variable name")
}
