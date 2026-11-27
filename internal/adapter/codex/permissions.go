package codex

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	profileName       = "promptstor"
	profileTable      = permissionsKey + "." + profileName
	defaultProfileKey = "default_permissions"
	sandboxModeKey    = "sandbox_mode"
	extendsKey        = "extends"
	filesystemKey     = "filesystem"
	workspaceBase     = ":workspace"
	workspaceRoots    = ":workspace_roots"
	denyAccess        = "deny"
	justification     = "promptstor: "
	wildcard          = "*"
	homePrefix        = "~/"

	reasonNoProfile    = "Codex path denies need default_permissions, which PromptStor cannot set"
	reasonOwnProfile   = "[permissions.promptstor] holds settings PromptStor does not write"
	reasonUnconfirmed  = "Codex profile denies are not confirmed to cover apply_patch and built-in file tools"
	reasonDenyCommand  = "deny command %q cannot be written as a Codex prefix_rule, which matches whole words"
	reasonDenyPath     = "deny path %q cannot be written in a Codex permission profile"
	reasonBarePath     = "deny path %q would match only at the workspace root in Codex; write it as **/%s"
	reasonNoAllowList  = "Codex has no MCP server allow list"
	reasonNoNativePerm = "Codex has no native permission entries"
	reasonNothing      = "nothing in this permission applies to Codex"
)

var shellOperators = []string{"|", "||", "&&", ";", "&"}

type profilePaths struct {
	anywhere, workspace []string
	items               []library.ID
}

func (ex *exporter) permissions(items []library.Item) error {
	var rules strings.Builder
	var ruleItems []library.ID
	var denying []library.Item
	for _, it := range items {
		ex.reportUnmapped(it)
		if it.Portable == nil {
			continue
		}
		if lines := ex.prefixRules(it); lines != "" {
			rules.WriteString(lines)
			ruleItems = append(ruleItems, it.ID)
		}
		if len(it.Portable.DenyPaths) > 0 {
			denying = append(denying, it)
		}
	}
	if rules.Len() > 0 {
		ex.add(plan.Unit{
			Path:    ex.l.in(rulesDir, ownRules),
			Kind:    manifest.UnitFile,
			Content: []byte(generatedHeader + rules.String()),
			Items:   ruleItems,
		})
	}
	return ex.profile(denying)
}

func (ex *exporter) reportUnmapped(it library.Item) {
	_, native := it.Native[tool]
	var commands, paths, servers int
	if p := it.Portable; p != nil {
		commands, paths, servers = len(p.DenyCommands), len(p.DenyPaths), len(p.AllowMCPServers)
	}
	if servers > 0 {
		ex.report(it, reasonNoAllowList)
	}
	switch {
	case native:
		ex.report(it, reasonNoNativePerm)
	case commands+paths+servers == 0:
		ex.report(it, reasonNothing)
	}
}

func (ex *exporter) prefixRules(it library.Item) string {
	var lines strings.Builder
	for _, command := range it.Portable.DenyCommands {
		tokens, ok := argv(command)
		if !ok {
			ex.block(it, fmt.Sprintf(reasonDenyCommand, command))
			continue
		}
		quoted := make([]string, len(tokens))
		for i, token := range tokens {
			quoted[i] = strconv.Quote(token)
		}
		fmt.Fprintf(&lines, "prefix_rule(pattern=[%s], decision=\"forbidden\", justification=%s)\n",
			strings.Join(quoted, ", "), strconv.Quote(justification+it.Name))
	}
	return lines.String()
}

func argv(command string) ([]string, bool) {
	tokens := strings.Fields(command)
	if len(tokens) > 0 && tokens[len(tokens)-1] == wildcard {
		tokens = tokens[:len(tokens)-1]
	}
	unmatchable := slices.ContainsFunc(tokens, func(token string) bool {
		return strings.ContainsAny(token, `*"'`) || slices.Contains(shellOperators, token)
	})
	return tokens, len(tokens) > 0 && !unmatchable
}

func (ex *exporter) profile(items []library.Item) error {
	reason, err := ex.profileObstacle()
	if err != nil {
		return err
	}
	if reason != "" {
		for _, it := range items {
			ex.block(it, reason)
		}
		return nil
	}
	var collected profilePaths
	for _, it := range items {
		ex.collectPaths(it, &collected)
	}
	for _, it := range items {
		if slices.Contains(collected.items, it.ID) {
			ex.block(it, reasonUnconfirmed)
		}
	}
	ex.add(plan.Unit{
		Path:    ex.l.in(configName),
		Kind:    manifest.UnitTOMLTable,
		Key:     profileTable,
		Content: renderProfile(collected),
		Mode:    configMode,
		Items:   collected.items,
	})
	return nil
}

func (ex *exporter) profileObstacle() (string, error) {
	doc, valid, err := readConfig(ex.l.in(configName))
	if err != nil {
		return "", err
	}
	if !valid || doc[defaultProfileKey] != profileName {
		return reasonNoProfile, nil
	}
	layers := []map[string]any{doc}
	if ex.l.scope == adapter.ScopeProject {
		user, valid, err := readConfig(filepath.Join(codexHome(ex.t.Env), configName))
		if err != nil {
			return "", err
		}
		if !valid {
			return reasonNoProfile, nil
		}
		layers = append(layers, user)
	}
	for _, layer := range layers {
		if _, set := layer[sandboxModeKey]; set {
			return reasonNoProfile, nil
		}
	}
	if !onlyDenies(subtable(subtable(doc, permissionsKey), profileName)) {
		return reasonOwnProfile, nil
	}
	return "", nil
}

func onlyDenies(profile map[string]any) bool {
	for key, value := range profile {
		switch key {
		case extendsKey:
			if value != workspaceBase {
				return false
			}
		case filesystemKey:
			entries, ok := value.(map[string]any)
			if !ok {
				return false
			}
			for _, entry := range entries {
				scoped, nested := entry.(map[string]any)
				if !nested {
					scoped = map[string]any{"": entry}
				}
				for _, access := range scoped {
					if access != denyAccess {
						return false
					}
				}
			}
		default:
			return false
		}
	}
	return true
}

func (ex *exporter) collectPaths(it library.Item, collected *profilePaths) {
	added := false
	for _, path := range it.Portable.DenyPaths {
		segments := strings.Split(path, "/")
		switch {
		case strings.HasPrefix(path, "!") || strings.HasPrefix(path, ":") || slices.Contains(segments, ".."):
			ex.block(it, fmt.Sprintf(reasonDenyPath, path))
			continue
		case strings.HasPrefix(path, "/") || strings.HasPrefix(path, homePrefix):
			collected.anywhere = append(collected.anywhere, path)
		case strings.HasPrefix(path, "~") || segments[0] == "." || path == "":
			ex.block(it, fmt.Sprintf(reasonDenyPath, path))
			continue
		case len(segments) == 1:
			ex.block(it, fmt.Sprintf(reasonBarePath, path, path))
			continue
		default:
			collected.workspace = append(collected.workspace, path)
		}
		added = true
	}
	if added {
		collected.items = append(collected.items, it.ID)
	}
}

func renderProfile(collected profilePaths) []byte {
	var block strings.Builder
	fmt.Fprintf(&block, "[%s]\n%s = %s", profileTable, extendsKey, tomlString(workspaceBase))
	denies := func(header string, paths []string) {
		if len(paths) == 0 {
			return
		}
		fmt.Fprintf(&block, "\n\n[%s]", header)
		for _, path := range slices.Compact(slices.Sorted(slices.Values(paths))) {
			fmt.Fprintf(&block, "\n%s = %s", tomlString(path), tomlString(denyAccess))
		}
	}
	denies(profileTable+"."+filesystemKey, collected.anywhere)
	denies(profileTable+"."+filesystemKey+"."+tomlString(workspaceRoots), collected.workspace)
	return []byte(block.String())
}
