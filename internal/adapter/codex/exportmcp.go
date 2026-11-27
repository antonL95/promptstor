package codex

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/secrets"
)

const (
	reasonNoServer    = "the item has no server definition"
	reasonServerName  = "Codex rejects the server name; use letters, numbers, - _ : @ / ."
	reasonResolve     = "resolve: true would write the secret value; set the server up by hand"
	reasonLiteral     = "holds a literal secret, which PromptStor never writes to config.toml"
	reasonPlaceholder = "a variable in %s cannot be expanded by Codex; set the server up by hand"
	reasonNeedsSetup  = "secret %s needs setup before Codex can use it"
	reasonStdio       = "a stdio server needs a command and no url or headers"
	reasonHTTP        = "an http server needs a url and no command, args or env"
	reasonTransport   = "transport %q is not supported by Codex"
	reasonWouldDrop   = "[mcp_servers.%s] has %s, which PromptStor would drop; set the server up by hand"
	reasonText        = "the server is not valid UTF-8"
)

var (
	serverName       = regexp.MustCompile(`^[A-Za-z0-9_:@/.-]+$`)
	placeholder      = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)
	wholePlaceholder = regexp.MustCompile(`^` + placeholder.String() + `$`)
)

type nativeServer struct {
	command, url, bearer     string
	args, envVars            []string
	env, headers, envHeaders map[string]string
}

func (ex *exporter) servers(items []library.Item) error {
	if len(items) == 0 {
		return nil
	}
	doc, _, err := readConfig(ex.l.in(configName))
	if err != nil {
		return err
	}
	existing := subtable(doc, serversKey)
	for _, it := range items {
		ex.server(it, subtable(existing, it.Name))
	}
	return nil
}

func (ex *exporter) server(it library.Item, existing map[string]any) {
	switch {
	case it.MCP == nil:
		ex.report(it, reasonNoServer)
		return
	case !serverName.MatchString(it.Name):
		ex.report(it, reasonServerName)
		return
	}
	server, err := nativeServerOf(*it.MCP)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	if extra := unknownSettings(existing); len(extra) > 0 {
		ex.report(it, fmt.Sprintf(reasonWouldDrop, it.Name, strings.Join(extra, ", ")))
		return
	}
	var enabled *bool
	if value, ok := existing[fieldEnabled].(bool); ok {
		enabled = &value
	}
	table := serversKey + "." + tomlKey(it.Name)
	content := server.render(table, enabled)
	if !utf8.Valid(content) {
		ex.report(it, reasonText)
		return
	}
	ex.add(plan.Unit{
		Path:    ex.l.in(configName),
		Kind:    manifest.UnitTOMLTable,
		Key:     table,
		Content: content,
		Mode:    configMode,
		Items:   []library.ID{it.ID},
	})
}

func unknownSettings(existing map[string]any) []string {
	var extra []string
	for _, key := range slices.Sorted(maps.Keys(existing)) {
		if !slices.Contains(serverFields, key) {
			extra = append(extra, key)
		}
	}
	variables, _ := existing[fieldEnvVars].([]any)
	if slices.ContainsFunc(variables, func(entry any) bool { _, name := entry.(string); return !name }) {
		extra = append(extra, fieldEnvVars+" sources")
	}
	return extra
}

func nativeServerOf(s library.MCPServer) (nativeServer, error) {
	switch s.Transport {
	case transportStdio:
		if s.Command == "" || s.URL != "" || len(s.Headers) > 0 {
			return nativeServer{}, errors.New(reasonStdio)
		}
	case transportHTTP:
		if s.URL == "" || s.Command != "" || len(s.Args) > 0 || len(s.Env) > 0 {
			return nativeServer{}, errors.New(reasonHTTP)
		}
	default:
		return nativeServer{}, fmt.Errorf(reasonTransport, s.Transport)
	}
	if s.Resolve {
		return nativeServer{}, errors.New(reasonResolve)
	}
	if _, found := secrets.Scrub(s); len(found) > 0 {
		return nativeServer{}, errors.New(reasonLiteral)
	}
	for _, plain := range []struct {
		name  string
		texts []string
	}{{fieldCommand, []string{s.Command}}, {fieldArgs, s.Args}, {fieldURL, []string{s.URL}}} {
		if slices.ContainsFunc(plain.texts, placeholder.MatchString) {
			return nativeServer{}, fmt.Errorf(reasonPlaceholder, plain.name)
		}
	}
	out := nativeServer{command: s.Command, args: s.Args, url: s.URL, env: map[string]string{}, headers: map[string]string{}, envHeaders: map[string]string{}}
	if err := out.mapEnv(s.Env); err != nil {
		return nativeServer{}, err
	}
	if err := out.mapHeaders(s.Headers); err != nil {
		return nativeServer{}, err
	}
	return out, nil
}

func textOf(key string, value library.Value) (string, error) {
	if value.Ref == "" {
		return value.Literal, nil
	}
	if name, found := strings.CutPrefix(value.Ref, envScheme); found && wholePlaceholder.MatchString(envRef(name)) {
		return envRef(name), nil
	}
	return "", fmt.Errorf(reasonNeedsSetup, key)
}

func (n *nativeServer) mapEnv(values map[string]library.Value) error {
	for _, name := range slices.Sorted(maps.Keys(values)) {
		key := fieldEnv + "/" + name
		text, err := textOf(key, values[name])
		switch {
		case err != nil:
			return err
		case text == envRef(name):
			n.envVars = append(n.envVars, name)
		case placeholder.MatchString(text):
			return fmt.Errorf(reasonPlaceholder, key)
		default:
			n.env[name] = text
		}
	}
	return nil
}

func (n *nativeServer) mapHeaders(values map[string]library.Value) error {
	for _, name := range slices.Sorted(maps.Keys(values)) {
		key := "headers/" + name
		text, err := textOf(key, values[name])
		if err != nil {
			return err
		}
		token, bearer := strings.CutPrefix(text, bearerPrefix)
		switch {
		case name == authorizationHeader && bearer && wholePlaceholder.MatchString(token):
			n.bearer = wholePlaceholder.FindStringSubmatch(token)[1]
		case wholePlaceholder.MatchString(text):
			n.envHeaders[name] = wholePlaceholder.FindStringSubmatch(text)[1]
		case placeholder.MatchString(text):
			return fmt.Errorf(reasonPlaceholder, key)
		default:
			n.headers[name] = text
		}
	}
	return nil
}

func (n nativeServer) render(table string, enabled *bool) []byte {
	var block strings.Builder
	block.WriteString("[" + table + "]")
	line := func(key, value string) { block.WriteString("\n" + key + " = " + value) }
	text := func(key, value string) {
		if value != "" {
			line(key, tomlString(value))
		}
	}
	list := func(key string, values []string) {
		if len(values) > 0 {
			line(key, tomlArray(values))
		}
	}
	inline := func(key string, values map[string]string) {
		if len(values) > 0 {
			line(key, tomlInlineTable(values))
		}
	}
	text(fieldCommand, n.command)
	list(fieldArgs, n.args)
	text(fieldURL, n.url)
	inline(fieldEnv, n.env)
	list(fieldEnvVars, n.envVars)
	text(fieldBearer, n.bearer)
	inline(fieldHeaders, n.headers)
	inline(fieldEnvHeaders, n.envHeaders)
	if enabled != nil {
		line(fieldEnabled, strconv.FormatBool(*enabled))
	}
	return []byte(block.String())
}
