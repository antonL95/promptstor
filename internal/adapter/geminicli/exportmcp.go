package geminicli

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

const (
	envScheme        = "env:"
	reasonNoServer   = "the item has no server definition"
	reasonStdio      = "a stdio server needs a command and no url or headers"
	reasonRemote     = "an %s server needs a url and no command, args or env"
	reasonTransport  = "transport %q is not supported by Gemini CLI"
	reasonNeedsSetup = "secret %s/%s needs setup before Gemini CLI can use it"
	reasonWouldDrop  = "mcpServers/%s has %s, which PromptStor would drop; set the server up by hand"
)

var canonicalPlaceholder = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

func (ex *exporter) server(it library.Item) {
	if it.MCP == nil {
		ex.report(it, reasonNoServer)
		return
	}
	rendered, reason := renderServer(*it.MCP)
	if reason != "" {
		ex.report(it, reason)
		return
	}
	var extra []string
	for _, field := range slices.Sorted(maps.Keys(object(object(ex.existing, serversKey), it.Name))) {
		if !slices.Contains(serverFields, field) {
			extra = append(extra, field)
		}
	}
	if len(extra) > 0 {
		ex.report(it, fmt.Sprintf(reasonWouldDrop, it.Name, strings.Join(extra, ", ")))
		return
	}
	content, err := marshal(rendered)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	ex.add(plan.Unit{
		Path:    ex.l.settings,
		Kind:    manifest.UnitJSON,
		Key:     "/" + serversKey + "/" + escapePointer(it.Name),
		Content: content,
		Items:   []library.ID{it.ID},
	})
}

func renderServer(s library.MCPServer) (serverDoc, string) {
	out := serverDoc{Command: nativeText(s.Command), URL: nativeText(s.URL)}
	switch s.Transport {
	case transportStdio:
		if s.Command == "" || s.URL != "" || len(s.Headers) > 0 {
			return serverDoc{}, reasonStdio
		}
	case transportHTTP, transportSSE:
		if s.URL == "" || s.Command != "" || len(s.Args) > 0 || len(s.Env) > 0 {
			return serverDoc{}, fmt.Sprintf(reasonRemote, s.Transport)
		}
		out.Type = s.Transport
	default:
		return serverDoc{}, fmt.Sprintf(reasonTransport, s.Transport)
	}
	for _, arg := range s.Args {
		out.Args = append(out.Args, nativeText(arg))
	}
	var reason string
	if out.Env, reason = nativeValues("env", s.Env); reason != "" {
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

func nativeText(text string) string {
	return canonicalPlaceholder.ReplaceAllStringFunc(text, func(placeholder string) string {
		return envRef(canonicalPlaceholder.FindStringSubmatch(placeholder)[1])
	})
}

func escapePointer(token string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(token)
}
