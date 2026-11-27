package cursor

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	serversKey       = "mcpServers"
	typeField        = "type"
	commandField     = "command"
	argsField        = "args"
	envField         = "env"
	urlField         = "url"
	headersField     = "headers"
	envFileField     = "envFile"
	transportStdio   = "stdio"
	transportHTTP    = "http"
	transportSSE     = "sse"
	envScheme        = "env:"
	reasonNoServer   = "the item has no server definition"
	reasonStdio      = "a stdio server needs a command and no url or headers"
	reasonRemote     = "a remote server needs a url and no command, args or env"
	reasonTransport  = "transport %q is not supported by Cursor"
	reasonNeedsSetup = "secret %s/%s needs setup before Cursor can use it"
	reasonWouldDrop  = "mcpServers/%s has %s, which PromptStor would drop; set the server up by hand"
)

var renderedFields = []string{typeField, commandField, argsField, envField, urlField, headersField}

type serverDoc struct {
	Type    json.RawMessage   `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	EnvFile json.RawMessage   `json:"envFile,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (ex *exporter) server(it library.Item, existing map[string]json.RawMessage) {
	if it.MCP == nil {
		ex.report(it, reasonNoServer)
		return
	}
	rendered, reason := renderServer(*it.MCP)
	if reason != "" {
		ex.report(it, reason)
		return
	}
	current := rawObject(existing[it.Name])
	stdio := rendered.Command != ""
	var lost []string
	for _, field := range slices.Sorted(maps.Keys(current)) {
		if !slices.Contains(renderedFields, field) && (field != envFileField || !stdio) {
			lost = append(lost, field)
		}
	}
	if len(lost) > 0 {
		ex.report(it, fmt.Sprintf(reasonWouldDrop, it.Name, strings.Join(lost, ", ")))
		return
	}
	_, typed := current[typeField]
	_, local := current[commandField]
	switch {
	case stdio:
		rendered.EnvFile = current[envFileField]
		if local && !typed {
			rendered.Type = nil
		}
	case typed && !local:
		rendered.Type = current[typeField]
	}
	content, err := marshal(rendered)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	ex.add(plan.Unit{
		Path:    ex.l.in(mcpName),
		Kind:    manifest.UnitJSON,
		Key:     "/" + serversKey + "/" + escapePointer(it.Name),
		Content: content,
		Items:   []library.ID{it.ID},
	})
}

func renderServer(s library.MCPServer) (serverDoc, string) {
	out := serverDoc{Command: s.Command, Args: s.Args, URL: s.URL}
	switch s.Transport {
	case transportStdio:
		if s.Command == "" || s.URL != "" || len(s.Headers) > 0 {
			return serverDoc{}, reasonStdio
		}
		out.Type = json.RawMessage(`"` + transportStdio + `"`)
	case transportHTTP, transportSSE:
		if s.URL == "" || s.Command != "" || len(s.Args) > 0 || len(s.Env) > 0 {
			return serverDoc{}, reasonRemote
		}
	default:
		return serverDoc{}, fmt.Sprintf(reasonTransport, s.Transport)
	}
	var reason string
	if out.Env, reason = nativeValues(envField, s.Env); reason != "" {
		return serverDoc{}, reason
	}
	if out.Headers, reason = nativeValues(headersField, s.Headers); reason != "" {
		return serverDoc{}, reason
	}
	return out, ""
}

func nativeValues(section string, values map[string]library.Value) (map[string]string, string) {
	if len(values) == 0 {
		return nil, ""
	}
	out := make(map[string]string, len(values))
	for _, name := range slices.Sorted(maps.Keys(values)) {
		value := values[name]
		variable, isEnv := strings.CutPrefix(value.Ref, envScheme)
		switch {
		case value.Ref == "":
			out[name] = value.Literal
		case isEnv && variable != "":
			out[name] = envRef(variable)
		default:
			return nil, fmt.Sprintf(reasonNeedsSetup, section, name)
		}
	}
	return out, ""
}
