package antigravity

import (
	"fmt"
	"path"
	"slices"
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
	commandAction  = "command"
	readAction     = "read_file"
	writeAction    = "write_file"
	regexPrefix    = "regex:"
	wildcard       = "*"
	homePrefix     = "~/"
	subtreeSuffix  = "/**"
	notInPath      = `*?[]{}()\`

	reasonAppOnly      = "not enforced in the Antigravity App and IDE"
	reasonProjectDeny  = "Antigravity has no project permission file"
	reasonDenyCommand  = "deny command %q cannot be written as an Antigravity command rule"
	reasonDenyPath     = "deny path %q cannot be written as an Antigravity file rule"
	reasonRelativePath = "deny path %q is matched from the workspace root only"
	reasonNoAllowList  = "Antigravity cannot restrict MCP servers to an allow list"
	reasonNoNativePerm = "Antigravity native permission entries are not supported"
	reasonNothing      = "nothing in this permission applies to Antigravity"
)

var shellOperators = []string{"|", "||", "&&", ";", "&"}

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
		ex.report(it, reasonNoNativePerm)
	case denies+len(portable.AllowMCPServers) == 0:
		ex.report(it, reasonNothing)
	}
	switch {
	case denies == 0:
	case ex.l.scope != adapter.ScopeUser:
		ex.block(it, reasonProjectDeny)
	default:
		written := len(ex.units)
		ex.denyCommands(it, portable.DenyCommands)
		ex.denyPaths(it, portable.DenyPaths)
		if len(ex.units) > written {
			ex.block(it, reasonAppOnly)
		}
	}
}

func (ex *exporter) denyCommands(it library.Item, commands []string) {
	for _, command := range commands {
		prefix, ok := commandPrefix(command)
		if !ok {
			ex.block(it, fmt.Sprintf(reasonDenyCommand, command))
			continue
		}
		ex.deny(it, commandAction, prefix)
	}
}

func (ex *exporter) denyPaths(it library.Item, globs []string) {
	for _, glob := range globs {
		target, ok := pathTarget(glob, ex.t.Env.Home)
		if !ok {
			ex.block(it, fmt.Sprintf(reasonDenyPath, glob))
			continue
		}
		ex.deny(it, readAction, target)
		ex.deny(it, writeAction, target)
		if !path.IsAbs(target) {
			ex.block(it, fmt.Sprintf(reasonRelativePath, glob))
		}
	}
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
	ex.add(plan.Unit{Path: ex.l.cliSettings(), Kind: manifest.UnitJSONElement, Key: key, Content: content, Items: []library.ID{it.ID}})
}

func commandPrefix(command string) (string, bool) {
	tokens := strings.Fields(command)
	if len(tokens) > 0 && tokens[len(tokens)-1] == wildcard {
		tokens = tokens[:len(tokens)-1]
	}
	unmatchable := slices.ContainsFunc(tokens, func(token string) bool {
		return strings.ContainsAny(token, "*\"'`()") || slices.Contains(shellOperators, token)
	})
	prefix := strings.Join(tokens, " ")
	return prefix, prefix != "" && !unmatchable && !strings.HasPrefix(prefix, regexPrefix) && utf8.ValidString(prefix)
}

func pathTarget(glob, home string) (string, bool) {
	trimmed := strings.TrimSuffix(glob, subtreeSuffix)
	switch {
	case !utf8.ValidString(trimmed) || strings.ContainsFunc(trimmed, unicode.IsControl):
		return "", false
	case strings.HasPrefix(trimmed, "!") || strings.ContainsAny(trimmed, notInPath):
		return "", false
	}
	if rest, underHome := strings.CutPrefix(trimmed, homePrefix); underHome {
		if !plainSegments(rest) {
			return "", false
		}
		return strings.TrimSuffix(home, "/") + "/" + rest, true
	}
	if trimmed == "~" && glob != trimmed {
		return home, true
	}
	if strings.HasPrefix(trimmed, "~") || !plainSegments(strings.TrimPrefix(trimmed, "/")) {
		return "", false
	}
	return trimmed, true
}

func plainSegments(relative string) bool {
	segments := strings.Split(strings.TrimSuffix(relative, "/"), "/")
	return !slices.ContainsFunc(segments, func(segment string) bool { return segment == "" || segment == "." || segment == ".." })
}
