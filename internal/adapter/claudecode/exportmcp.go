package claudecode

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const envScheme = "env:"

var canonicalPlaceholder = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

type serverJSON struct {
	Type    string            `json:"type"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (ex *exporter) server(it library.Item) {
	if it.MCP == nil {
		ex.report(it, "the item has no server definition")
		return
	}
	rendered, reason := renderServer(*it.MCP)
	if reason != "" {
		ex.report(it, reason)
		return
	}
	content, err := marshal(rendered)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	ex.add(plan.Unit{
		Path:    ex.l.mcp,
		Kind:    manifest.UnitJSON,
		Key:     mcpServersPointer + "/" + escapePointer(it.Name),
		Content: content,
		Mode:    fileMode,
		Items:   []library.ID{it.ID},
	})
}

func renderServer(s library.MCPServer) (serverJSON, string) {
	switch s.Transport {
	case transportStdio:
		if s.Command == "" || s.URL != "" {
			return serverJSON{}, "a stdio server needs a command and no url"
		}
	case transportHTTP, transportSSE:
		if s.URL == "" || s.Command != "" || len(s.Args) > 0 || len(s.Env) > 0 {
			return serverJSON{}, "an " + s.Transport + " server needs a url and no command, args or env"
		}
	default:
		return serverJSON{}, fmt.Sprintf("transport %q is not supported by Claude Code", s.Transport)
	}
	out := serverJSON{Type: s.Transport, Command: nativeText(s.Command), URL: nativeText(s.URL)}
	for _, arg := range s.Args {
		out.Args = append(out.Args, nativeText(arg))
	}
	var reason string
	if out.Env, reason = nativeValues("env", s.Env); reason != "" {
		return serverJSON{}, reason
	}
	if out.Headers, reason = nativeValues("headers", s.Headers); reason != "" {
		return serverJSON{}, reason
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
			return nil, fmt.Sprintf("secret %s/%s needs setup before Claude Code can use it", section, name)
		}
	}
	return out, ""
}

func nativeText(text string) string {
	return canonicalPlaceholder.ReplaceAllStringFunc(text, func(placeholder string) string {
		return envRef(canonicalPlaceholder.FindStringSubmatch(placeholder)[1])
	})
}
