package antigravity

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	serversKey       = "mcpServers"
	transportStdio   = "stdio"
	transportHTTP    = "http"
	envScheme        = "env:"
	cwdField         = "cwd"
	reasonNoServer   = "the item has no server definition"
	reasonStdio      = "a stdio server needs a command and no url or headers"
	reasonRemote     = "an http server needs a url and no command, args or env"
	reasonTransport  = "transport %q is not supported by Antigravity"
	reasonNoEnvRef   = "Antigravity does not expand environment variables in mcp_config.json"
	reasonNeedsSetup = "secret %s/%s needs setup before Antigravity can use it"
	reasonWouldDrop  = "mcpServers/%s has %s, which PromptStor would drop; set the server up by hand"
)

var (
	canonicalPlaceholder = regexp.MustCompile(`\$\{env:[A-Za-z_][A-Za-z0-9_]*\}`)
	renderedFields       = []string{"command", "args", "env", "serverUrl", "url", "headers"}
	controlFields        = []string{cwdField, "disabled", "enabledTools", "disabledTools", "tools", "timeoutSeconds"}
)

type serverDoc struct {
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	ServerURL string            `json:"serverUrl,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`

	Cwd            json.RawMessage `json:"cwd,omitempty"`
	Disabled       json.RawMessage `json:"disabled,omitempty"`
	EnabledTools   json.RawMessage `json:"enabledTools,omitempty"`
	DisabledTools  json.RawMessage `json:"disabledTools,omitempty"`
	Tools          json.RawMessage `json:"tools,omitempty"`
	TimeoutSeconds json.RawMessage `json:"timeoutSeconds,omitempty"`
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
	var lost []string
	for _, field := range slices.Sorted(maps.Keys(current)) {
		kept := slices.Contains(controlFields, field) && (field != cwdField || rendered.Command != "")
		if !kept && !slices.Contains(renderedFields, field) {
			lost = append(lost, field)
		}
	}
	if len(lost) > 0 {
		ex.report(it, fmt.Sprintf(reasonWouldDrop, it.Name, strings.Join(lost, ", ")))
		return
	}
	rendered.Cwd, rendered.Disabled, rendered.Tools = current[cwdField], current["disabled"], current["tools"]
	rendered.EnabledTools, rendered.DisabledTools = current["enabledTools"], current["disabledTools"]
	rendered.TimeoutSeconds = current["timeoutSeconds"]
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
	out := serverDoc{Command: s.Command, Args: s.Args, ServerURL: s.URL}
	switch s.Transport {
	case transportStdio:
		if s.Command == "" || s.URL != "" || len(s.Headers) > 0 {
			return serverDoc{}, reasonStdio
		}
	case transportHTTP:
		if s.URL == "" || s.Command != "" || len(s.Args) > 0 || len(s.Env) > 0 {
			return serverDoc{}, reasonRemote
		}
	default:
		return serverDoc{}, fmt.Sprintf(reasonTransport, s.Transport)
	}
	if slices.ContainsFunc(append([]string{s.Command, s.URL}, s.Args...), canonicalPlaceholder.MatchString) {
		return serverDoc{}, reasonNoEnvRef
	}
	var reason string
	if out.Env, reason = literalValues("env", s.Env); reason != "" {
		return serverDoc{}, reason
	}
	if out.Headers, reason = literalValues("headers", s.Headers); reason != "" {
		return serverDoc{}, reason
	}
	return out, ""
}

func literalValues(section string, values map[string]library.Value) (map[string]string, string) {
	if len(values) == 0 {
		return nil, ""
	}
	out := make(map[string]string, len(values))
	for _, name := range slices.Sorted(maps.Keys(values)) {
		value := values[name]
		switch {
		case strings.HasPrefix(value.Ref, envScheme) || canonicalPlaceholder.MatchString(value.Literal):
			return nil, reasonNoEnvRef
		case value.Ref != "":
			return nil, fmt.Sprintf(reasonNeedsSetup, section, name)
		}
		out[name] = value.Literal
	}
	return out, ""
}

func escapePointer(token string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(token)
}
