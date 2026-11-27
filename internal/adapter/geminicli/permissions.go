package geminicli

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	shellTool    = "run_shell_command"
	denyPriority = 999
	wildcard     = "*"

	reasonProjectDeny  = "project policy files are ignored by Gemini CLI"
	reasonPartialPath  = "Gemini CLI path denies match only the file_path text of read_file, write_file and replace; shell, search and bulk read tools are not covered"
	reasonDenyCommand  = "deny command %q cannot be written as a Gemini CLI commandPrefix"
	reasonDenyPath     = "deny path %q cannot be written as a Gemini CLI argsPattern"
	reasonNoAllowList  = "mcp.allowed is not set, and writing it would block every other MCP server"
	reasonNoNativePerm = "Gemini CLI native permission entries are not supported"
	reasonNothing      = "nothing in this permission applies to Gemini CLI"
)

var (
	filePathTools  = []string{"read_file", "write_file", "replace"}
	shellOperators = []string{"|", "||", "&&", ";", "&"}
)

type policyFile struct {
	rules strings.Builder
	items []library.ID
}

func (p *policyFile) add(it library.Item, fields ...string) {
	p.rules.WriteString("\n[[rule]]\n")
	for _, field := range fields {
		p.rules.WriteString(field + "\n")
	}
	fmt.Fprintf(&p.rules, "decision = \"deny\"\npriority = %d\n", denyPriority)
	if !slices.Contains(p.items, it.ID) {
		p.items = append(p.items, it.ID)
	}
}

func (ex *exporter) permission(it library.Item, policy *policyFile) {
	_, native := it.Native[tool]
	var portable library.PortablePermission
	if it.Portable != nil {
		portable = *it.Portable
	}
	denies := len(portable.DenyCommands) + len(portable.DenyPaths)
	switch {
	case native:
		ex.report(it, reasonNoNativePerm)
	case denies+len(portable.AllowMCPServers) == 0:
		ex.report(it, reasonNothing)
	}
	ex.allow(it, portable.AllowMCPServers)
	switch {
	case denies == 0:
	case ex.l.scope != adapter.ScopeUser:
		ex.block(it, reasonProjectDeny)
	default:
		ex.denyCommands(it, portable.DenyCommands, policy)
		ex.denyPaths(it, portable.DenyPaths, policy)
	}
}

func (ex *exporter) denyCommands(it library.Item, commands []string, policy *policyFile) {
	for _, command := range commands {
		prefix, ok := commandPrefix(command)
		if !ok {
			ex.block(it, fmt.Sprintf(reasonDenyCommand, command))
			continue
		}
		policy.add(it, "toolName = "+tomlString(shellTool), "commandPrefix = "+tomlString(prefix))
	}
}

func commandPrefix(command string) (string, bool) {
	tokens := strings.Fields(command)
	if len(tokens) > 0 && tokens[len(tokens)-1] == wildcard {
		tokens = tokens[:len(tokens)-1]
	}
	unmatchable := slices.ContainsFunc(tokens, func(token string) bool {
		return strings.ContainsAny(token, `*"'`) || slices.Contains(shellOperators, token)
	})
	prefix := strings.Join(tokens, " ")
	return prefix, prefix != "" && !unmatchable && utf8.ValidString(prefix)
}

func (ex *exporter) denyPaths(it library.Item, globs []string, policy *policyFile) {
	written := false
	for _, glob := range globs {
		pattern, ok := argsPattern(glob, ex.t.Env.Home)
		if !ok {
			ex.block(it, fmt.Sprintf(reasonDenyPath, glob))
			continue
		}
		policy.add(it, "toolName = "+tomlArray(filePathTools), "argsPattern = "+tomlString(pattern))
		written = true
	}
	if written {
		ex.block(it, reasonPartialPath)
	}
}

func (ex *exporter) policy(policy policyFile) {
	if len(policy.items) == 0 {
		return
	}
	ex.add(plan.Unit{
		Path:    ex.l.in(policiesDir, ownPolicy),
		Kind:    manifest.UnitFile,
		Content: []byte(generatedHeader + policy.rules.String()),
		Items:   policy.items,
	})
}

func (ex *exporter) allow(it library.Item, servers []string) {
	if len(servers) == 0 {
		return
	}
	if len(textList(object(ex.existing, mcpKey)[allowedKey])) == 0 {
		ex.report(it, reasonNoAllowList)
		return
	}
	for _, name := range servers {
		key, err := adapter.ElementKey(allowedServers, "", name)
		if err != nil {
			ex.report(it, err.Error())
			continue
		}
		content, err := marshal(name)
		if err != nil {
			ex.report(it, err.Error())
			continue
		}
		ex.add(plan.Unit{Path: ex.l.settings, Kind: manifest.UnitJSONElement, Key: key, Content: content, Items: []library.ID{it.ID}})
	}
}
