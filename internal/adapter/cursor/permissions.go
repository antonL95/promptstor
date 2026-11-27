package cursor

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	permissionsKey = "permissions"
	denyPointer    = "/permissions/deny"
	allowPointer   = "/permissions/allow"
	shellAction    = "Shell"
	readAction     = "Read"
	writeAction    = "Write"
	wildcard       = "*"
	homeShorthand  = "~"
	homePrefix     = "~/"
	notInCommand   = "*?:()\"'`|&;<>$\\"
	notInPath      = "{}[]()$\\"

	reasonEditor      = "not enforced in the Cursor editor"
	reasonDenyCommand = "deny command %q cannot be written; Cursor Shell rules match the first word of a command"
	reasonDenyPath    = "deny path %q cannot be written as a Cursor file rule"
	reasonCLIMoved    = "the Cursor CLI may read cli-config.json from another folder here; denies not written"
	reasonNoAllowList = "Cursor cannot restrict MCP servers to an allow list"
	reasonNoNative    = "Cursor native permission entries are not supported"
	reasonNothing     = "nothing in this permission applies to Cursor"
)

func (ex *exporter) permission(it library.Item) {
	_, native := it.Native[tool]
	var portable library.PortablePermission
	if it.Portable != nil {
		portable = *it.Portable
	}
	denies := len(portable.DenyCommands) + len(portable.DenyPaths)
	if len(portable.AllowMCPServers) > 0 {
		ex.report(it, reasonNoAllowList)
	}
	switch {
	case native:
		ex.report(it, reasonNoNative)
	case denies+len(portable.AllowMCPServers) == 0:
		ex.report(it, reasonNothing)
	}
	switch {
	case denies == 0:
		return
	case cliConfigMoved(ex.t):
		ex.block(it, reasonCLIMoved)
		return
	}
	written := len(ex.units)
	ex.denyCommands(it, portable.DenyCommands)
	ex.denyPaths(it, portable.DenyPaths)
	if len(ex.units) > written {
		ex.block(it, reasonEditor)
	}
}

func (ex *exporter) denyCommands(it library.Item, commands []string) {
	for _, command := range commands {
		base, ok := commandBase(command)
		if !ok {
			ex.block(it, fmt.Sprintf(reasonDenyCommand, command))
			continue
		}
		ex.deny(it, shellAction, base)
	}
}

func commandBase(command string) (string, bool) {
	tokens := strings.Fields(command)
	for len(tokens) > 0 && tokens[len(tokens)-1] == wildcard {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) != 1 || !printable(command) || strings.ContainsAny(tokens[0], notInCommand) {
		return "", false
	}
	return tokens[0], true
}

func printable(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

func (ex *exporter) denyPaths(it library.Item, globs []string) {
	for _, glob := range globs {
		target, ok := ex.pathTarget(glob)
		if !ok {
			ex.block(it, fmt.Sprintf(reasonDenyPath, glob))
			continue
		}
		ex.deny(it, readAction, target)
		ex.deny(it, writeAction, target)
	}
}

func (ex *exporter) pathTarget(glob string) (string, bool) {
	if !expressiblePath(glob) {
		return "", false
	}
	if !strings.HasPrefix(glob, homeShorthand) {
		return glob, true
	}
	if ex.l.scope != adapter.ScopeUser || (glob != homeShorthand && !strings.HasPrefix(glob, homePrefix)) {
		return "", false
	}
	return ex.t.Env.Home + strings.TrimPrefix(glob, homeShorthand), true
}

func expressiblePath(glob string) bool {
	return glob != "" && printable(glob) && !strings.HasPrefix(glob, "!") && !strings.HasSuffix(glob, "/") && !strings.ContainsAny(glob, notInPath)
}

func (ex *exporter) deny(it library.Item, action, target string) {
	value := action + "(" + target + ")"
	key, err := adapter.ElementKey(denyPointer, "", value)
	if err != nil {
		ex.block(it, err.Error())
		return
	}
	content, err := marshal(value)
	if err != nil {
		ex.block(it, err.Error())
		return
	}
	ex.add(plan.Unit{Path: ex.l.cliConfig(), Kind: manifest.UnitJSONElement, Key: key, Content: content, Items: []library.ID{it.ID}})
}
