package claudecode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	reasonNoNative      = "no claude-code native entry"
	reasonNoHookCommand = "Claude Code hooks are keyed by the command of their first handler, and this hook has none"
)

var eventName = regexp.MustCompile(`^[A-Za-z]+$`)

func (ex *exporter) permission(it library.Item) {
	var rules []struct{ list, rule string }
	addRule := func(list, rule string) { rules = append(rules, struct{ list, rule string }{list, rule}) }
	if p := it.Portable; p != nil {
		for _, command := range p.DenyCommands {
			addRule(denyList, bashRule(command))
		}
		for _, path := range p.DenyPaths {
			addRule(denyList, readRule(path))
			addRule(denyList, editRule(path))
		}
		for _, server := range p.AllowMCPServers {
			addRule(allowList, mcpRule(server))
		}
	}
	native, reason := nativePermissions(it.Native[tool])
	if reason != "" {
		ex.report(it, reason)
		return
	}
	for _, list := range permissionLists {
		for _, rule := range native[list] {
			addRule(list, rule)
		}
	}
	for _, r := range rules {
		key, err := adapter.ElementKey(permissionsPath+r.list, "", r.rule)
		if err != nil {
			ex.report(it, err.Error())
			return
		}
		content, err := marshal(r.rule)
		if err != nil {
			ex.report(it, err.Error())
			return
		}
		ex.add(plan.Unit{Path: ex.l.settings, Kind: manifest.UnitJSONElement, Key: key, Content: content, Mode: fileMode, Items: []library.ID{it.ID}})
	}
}

func nativePermissions(value any) (map[string][]string, string) {
	if value == nil {
		return nil, ""
	}
	lists, ok := value.(map[string]any)
	if !ok {
		return nil, "claude-code native permissions must map allow, deny and ask to lists of rules"
	}
	native := map[string][]string{}
	for _, key := range slices.Sorted(maps.Keys(lists)) {
		if !slices.Contains(permissionLists, key) {
			return nil, fmt.Sprintf("claude-code native permission key %s is not allow, deny or ask", key)
		}
		entries, ok := lists[key].([]any)
		if !ok {
			return nil, fmt.Sprintf("claude-code native permissions/%s must be a list of rules", key)
		}
		for _, entry := range entries {
			rule, ok := entry.(string)
			if !ok || rule == "" {
				return nil, fmt.Sprintf("claude-code native permissions/%s must be a list of rules", key)
			}
			native[key] = append(native[key], rule)
		}
	}
	return native, ""
}

func (ex *exporter) hook(it library.Item) {
	group, ok := it.Native[tool].(map[string]any)
	if !ok {
		ex.report(it, reasonNoNative)
		return
	}
	event, _ := group[eventField].(string)
	if !eventName.MatchString(event) {
		ex.report(it, "the claude-code hook has no valid event")
		return
	}
	command, ok := firstCommand(group)
	if !ok {
		ex.report(it, reasonNoHookCommand)
		return
	}
	pointer := hooksPath + event
	key, err := adapter.ElementKey(pointer, hookCommandField, command)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	body := maps.Clone(group)
	delete(body, eventField)
	content, err := marshal(body)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	ex.add(plan.Unit{Path: ex.l.settings, Kind: manifest.UnitJSONElement, Key: key, Content: content, Mode: fileMode, Items: []library.ID{it.ID}})
}

func firstCommand(group map[string]any) (string, bool) {
	handlers, ok := group["hooks"].([]any)
	if !ok || len(handlers) == 0 {
		return "", false
	}
	handler, ok := handlers[0].(map[string]any)
	if !ok {
		return "", false
	}
	command, ok := handler["command"].(string)
	return command, ok && command != ""
}

func marshal(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}
