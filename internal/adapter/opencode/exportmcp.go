package opencode

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	envScheme        = "env:"
	reasonNoServer   = "the item has no server definition"
	reasonStdio      = "a stdio server needs a command and no url or headers"
	reasonRemote     = "an http server needs a url and no command, args or env"
	reasonTransport  = "transport %q is not supported by opencode"
	reasonNeedsSetup = "secret %s/%s needs setup before opencode can use it"
	reasonReadsFile  = "the server uses {file:...}, which would make opencode read a local file into it"
	reasonWouldDrop  = "mcp server %s has %s, which PromptStor would drop; set the server up by hand"
	reasonShadowed   = "%s already defines mcp server %s and overrides the project file; not written"
	reasonNested     = "mcp.servers in opencode.jsonc is itself a server named servers; rename it first"
)

var canonicalPlaceholder = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

type serverDoc struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command,omitempty"`
	Cwd         json.RawMessage   `json:"cwd,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	OAuth       json.RawMessage   `json:"oauth,omitempty"`
	Disabled    json.RawMessage   `json:"disabled,omitempty"`
	Codemode    json.RawMessage   `json:"codemode,omitempty"`
	Timeout     json.RawMessage   `json:"timeout,omitempty"`
	Protocol    json.RawMessage   `json:"protocol,omitempty"`
	Pool        json.RawMessage   `json:"pool,omitempty"`
}

func (d *serverDoc) keep(key string, value any) bool {
	var slot *json.RawMessage
	switch {
	case key == cwdKey && d.Type == localType:
		slot = &d.Cwd
	case key == oauthKey && d.Type == remoteType && value == false:
		slot = &d.OAuth
	case key == disabledKey:
		slot = &d.Disabled
	case key == "codemode":
		slot = &d.Codemode
	case key == timeoutKey:
		slot = &d.Timeout
	case key == "protocol":
		slot = &d.Protocol
	case key == "pool":
		slot = &d.Pool
	default:
		return false
	}
	encoded, err := marshal(value)
	*slot = encoded
	return err == nil
}

func hasServerNamedServers(l layout, documents []document) bool {
	if l.unknownConfig {
		return false
	}
	return slices.ContainsFunc(documents, func(doc document) bool {
		return doc.path == l.at(jsoncName) && isServer(object(doc.fields[mcpKey])[serversKey])
	})
}

func (ex *exporter) server(it library.Item) {
	if it.MCP == nil {
		ex.report(it, reasonNoServer)
		return
	}
	existing := ex.servers[it.Name]
	switch {
	case ex.nested:
		ex.report(it, reasonNested)
		return
	case ex.l.scope == adapter.ScopeProject && filepath.Dir(existing.path) == ex.l.content:
		ex.report(it, fmt.Sprintf(reasonShadowed, projectDirName+"/"+filepath.Base(existing.path), it.Name))
		return
	}
	rendered, reason := renderServer(*it.MCP)
	if reason == "" && hasFileReference(it.Name) {
		reason = reasonReadsFile
	}
	if reason != "" {
		ex.report(it, reason)
		return
	}
	var lost []string
	for _, key := range slices.Sorted(maps.Keys(existing.fields)) {
		if !slices.Contains([]string{typeKey, commandKey, environmentKey, urlKey, headersKey}, key) && !rendered.keep(key, existing.fields[key]) {
			lost = append(lost, key)
		}
	}
	if len(lost) > 0 {
		ex.report(it, fmt.Sprintf(reasonWouldDrop, it.Name, strings.Join(lost, ", ")))
		return
	}
	content, err := marshal(rendered)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	ex.add(plan.Unit{
		Path:    ex.l.at(jsoncName),
		Kind:    manifest.UnitJSON,
		Key:     "/" + mcpKey + "/" + serversKey + "/" + escapePointer(it.Name),
		Content: content,
		Items:   []library.ID{it.ID},
	})
}

func renderServer(s library.MCPServer) (serverDoc, string) {
	var out serverDoc
	switch s.Transport {
	case transportStdio:
		if s.Command == "" || s.URL != "" || len(s.Headers) > 0 {
			return serverDoc{}, reasonStdio
		}
		out.Type = localType
		for _, part := range append([]string{s.Command}, s.Args...) {
			out.Command = append(out.Command, nativeText(part))
		}
	case transportHTTP:
		if s.URL == "" || s.Command != "" || len(s.Args) > 0 || len(s.Env) > 0 {
			return serverDoc{}, reasonRemote
		}
		out.Type, out.URL = remoteType, nativeText(s.URL)
	default:
		return serverDoc{}, fmt.Sprintf(reasonTransport, s.Transport)
	}
	if readsFile(s) {
		return serverDoc{}, reasonReadsFile
	}
	var reason string
	if out.Environment, reason = nativeValues("env", s.Env); reason != "" {
		return serverDoc{}, reason
	}
	if out.Headers, reason = nativeValues("headers", s.Headers); reason != "" {
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
			out[name] = nativeText(value.Literal)
		case isEnv && variable != "":
			out[name] = envRef(variable)
		default:
			return nil, fmt.Sprintf(reasonNeedsSetup, section, name)
		}
	}
	return out, ""
}

func nativeText(value string) string {
	return canonicalPlaceholder.ReplaceAllStringFunc(value, func(placeholder string) string {
		return envRef(canonicalPlaceholder.FindStringSubmatch(placeholder)[1])
	})
}
