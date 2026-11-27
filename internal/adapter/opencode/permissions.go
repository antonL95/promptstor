package opencode

import (
	"fmt"
	"os"
	"path/filepath"
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
	homeShorthand   = "~"
	homePrefix      = "~/"
	homeVariable    = "$HOME"
	rootGapPrefix   = "**/"
	notInPath       = "{}[]"
	notInCommand    = "\"'`|&;<>"
	reasonPartial   = "opencode path denies cover only the read and edit tools; shell and grep are not covered"
	reasonCommand   = "deny command %q cannot be written as an opencode shell rule"
	reasonPath      = "deny path %q cannot be written as an opencode path rule"
	reasonRootGap   = "deny path %q does not match at the top of the workspace"
	reasonRelative  = "deny path %q is matched against paths relative to the working directory only"
	reasonFolder    = "deny path %q names a folder, and opencode matches only that exact path; end it with /**"
	reasonAllowList = "opencode has no rule that allows one MCP server without also matching others"
	reasonNative    = "opencode native permission entries are not supported"
	reasonNothing   = "nothing in this permission applies to opencode"
)

func (ex *exporter) permission(it library.Item) {
	_, native := it.Native[tool]
	var portable library.PortablePermission
	if it.Portable != nil {
		portable = *it.Portable
	}
	if len(portable.AllowMCPServers) > 0 {
		ex.report(it, reasonAllowList)
	}
	switch {
	case native:
		ex.report(it, reasonNative)
	case len(portable.DenyCommands)+len(portable.DenyPaths)+len(portable.AllowMCPServers) == 0:
		ex.report(it, reasonNothing)
	}
	ex.denyCommands(it, portable.DenyCommands)
	ex.denyPaths(it, portable.DenyPaths)
}

func (ex *exporter) denyCommands(it library.Item, commands []string) {
	for _, command := range commands {
		prefix, ok := commandPrefix(command)
		if !ok {
			ex.block(it, fmt.Sprintf(reasonCommand, command))
			continue
		}
		ex.deny(it, shellAction, prefix+" "+anyResource)
	}
}

func commandPrefix(command string) (string, bool) {
	if !utf8.ValidString(command) || strings.ContainsFunc(command, unicode.IsControl) ||
		strings.ContainsAny(command, notInCommand) || substitutes(command) {
		return "", false
	}
	tokens := strings.Fields(command)
	for len(tokens) > 0 && tokens[len(tokens)-1] == anyResource {
		tokens = tokens[:len(tokens)-1]
	}
	prefix := strings.Join(tokens, " ")
	return prefix, strings.Trim(prefix, "*? ") != ""
}

func substitutes(value string) bool {
	return strings.Contains(value, "{env:") || strings.Contains(value, "{file:")
}

func (ex *exporter) denyPaths(it library.Item, globs []string) {
	written := false
	for _, glob := range globs {
		if !expressiblePath(glob) {
			ex.block(it, fmt.Sprintf(reasonPath, glob))
			continue
		}
		ex.deny(it, editAction, glob)
		ex.deny(it, readAction, glob)
		written = true
		switch {
		case strings.HasPrefix(glob, rootGapPrefix):
			ex.block(it, fmt.Sprintf(reasonRootGap, glob))
		case isRelative(glob):
			ex.block(it, fmt.Sprintf(reasonRelative, glob))
		}
		if ex.isFolder(glob) {
			ex.block(it, fmt.Sprintf(reasonFolder, glob))
		}
	}
	if written {
		ex.block(it, reasonPartial)
	}
}

func expressiblePath(glob string) bool {
	switch {
	case glob == "" || !utf8.ValidString(glob) || strings.ContainsFunc(glob, unicode.IsControl):
		return false
	case strings.HasPrefix(glob, "!") || strings.HasSuffix(glob, "/") || strings.ContainsAny(glob, notInPath):
		return false
	case strings.HasPrefix(glob, homeShorthand):
		return glob == homeShorthand || strings.HasPrefix(glob, homePrefix)
	default:
		return true
	}
}

func isRelative(glob string) bool {
	return !filepath.IsAbs(glob) && !slices.ContainsFunc([]string{homeShorthand, homeVariable, anyResource}, func(prefix string) bool {
		return strings.HasPrefix(glob, prefix)
	})
}

func (ex *exporter) isFolder(glob string) bool {
	if strings.ContainsAny(glob, "*?") {
		return false
	}
	path := glob
	if rest, ok := strings.CutPrefix(path, homeVariable); ok {
		path = homeShorthand + rest
	}
	path = ex.t.Env.Expand(path)
	if !filepath.IsAbs(path) {
		if ex.l.scope != adapter.ScopeProject {
			return false
		}
		path = filepath.Join(ex.t.ProjectRoot, path)
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (ex *exporter) deny(it library.Item, action, resource string) {
	content, err := marshal(permissionRule{Action: action, Resource: resource, Effect: effectDeny})
	if err != nil {
		ex.block(it, err.Error())
		return
	}
	hash, err := manifest.Hash(manifest.UnitJSONElement, content)
	if err != nil {
		ex.block(it, err.Error())
		return
	}
	ex.add(plan.Unit{
		Path:    ex.l.at(jsoncName),
		Kind:    manifest.UnitJSONElement,
		Key:     "/" + permissionsKey + "@#" + hash,
		Content: content,
		Items:   []library.ID{it.ID},
	})
}
