package claudecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
)

const (
	permissionName = "Claude Code permissions"
	eventField     = "event"
	matcherField   = "matcher"
)

type settingsDoc struct {
	Permissions map[string][]string         `json:"permissions"`
	Hooks       map[string][]map[string]any `json:"hooks"`
}

func (im *importer) readSettings(path string) (settingsDoc, bool, error) {
	var doc settingsDoc
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return doc, false, nil
	}
	if err != nil {
		return doc, false, fmt.Errorf("read %s: %w", path, err)
	}
	var raw struct {
		Permissions map[string]json.RawMessage  `json:"permissions"`
		Hooks       map[string][]map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		im.note(path, noteUnreadable, err)
		return doc, false, nil
	}
	doc.Hooks = raw.Hooks
	doc.Permissions = map[string][]string{}
	for _, list := range permissionLists {
		if value, ok := raw.Permissions[list]; ok {
			var entries []string
			if err := json.Unmarshal(value, &entries); err != nil {
				im.note(path, "permissions/%s: %v; not imported", list, err)
				continue
			}
			doc.Permissions[list] = entries
		}
	}
	return doc, true, nil
}

func (im *importer) settings() error {
	doc, found, err := im.readSettings(im.l.settings)
	if err != nil || !found {
		return err
	}
	im.grants(im.l.settings, allowList, doc.Permissions[allowList])
	if perm, ok := permissionItem(doc.Permissions[denyList], doc.Permissions[askList]); ok {
		perm.Source = im.l.settings
		im.out.Items = append(im.out.Items, perm)
	}
	im.hooks(doc.Hooks)
	return nil
}

func (im *importer) localSettings() error {
	if im.l.scope != adapter.ScopeProject {
		return nil
	}
	path := im.l.in(localSettingsName)
	doc, found, err := im.readSettings(path)
	if err != nil || !found {
		return err
	}
	for _, list := range permissionLists {
		im.grants(path, list, doc.Permissions[list])
	}
	return nil
}

func (im *importer) grants(path, list string, entries []string) {
	for _, entry := range entries {
		key, err := adapter.ElementKey(permissionsPath+list, "", entry)
		if err != nil {
			im.note(path, "permissions/%s entry %q: %v", list, entry, err)
			continue
		}
		im.out.Grants = append(im.out.Grants, adapter.Grant{Tool: tool, Path: path, Key: key, Value: entry})
	}
}

func permissionItem(deny, ask []string) (library.Item, bool) {
	portable := &library.PortablePermission{}
	var rest []any
	for _, rule := range deny {
		if command := bashPattern.FindStringSubmatch(rule); command != nil {
			portable.DenyCommands = append(portable.DenyCommands, command[1])
			continue
		}
		if path, ok := pairedPath(rule, deny); ok {
			portable.DenyPaths = append(portable.DenyPaths, path)
			continue
		}
		if _, ok := pairedPath(pairOf(rule), deny); ok {
			continue
		}
		rest = append(rest, rule)
	}
	native := map[string]any{}
	if len(rest) > 0 {
		native[denyList] = rest
	}
	if len(ask) > 0 {
		native[askList] = anyList(ask)
	}
	it := library.Item{Kind: library.KindPermission, Name: permissionName, Portable: portable}
	if len(native) > 0 {
		it.Native = map[string]any{tool: native}
	}
	empty := len(portable.DenyCommands) == 0 && len(portable.DenyPaths) == 0 && len(native) == 0
	return it, !empty
}

func pairedPath(rule string, deny []string) (string, bool) {
	path, ok := ruleArgument(rule, "Read")
	if !ok || !slices.Contains(deny, "Edit("+path+")") {
		return "", false
	}
	return portablePath(path)
}

func pairOf(rule string) string {
	if path, ok := ruleArgument(rule, "Edit"); ok {
		return "Read(" + path + ")"
	}
	return ""
}

func anyList(values []string) []any {
	list := make([]any, len(values))
	for i, value := range values {
		list[i] = value
	}
	return list
}

func (im *importer) hooks(events map[string][]map[string]any) {
	used := map[string]int{}
	for _, event := range slices.Sorted(maps.Keys(events)) {
		for _, group := range events[event] {
			native := maps.Clone(group)
			native[eventField] = event
			name := event
			if matcher, _ := group[matcherField].(string); matcher != "" {
				name += " " + matcher
			}
			if used[name]++; used[name] > 1 {
				name = fmt.Sprintf("%s %d", name, used[name])
			}
			im.out.Items = append(im.out.Items, library.Item{
				Kind:   library.KindHook,
				Name:   name,
				Source: im.l.settings,
				Native: map[string]any{tool: native},
			})
		}
	}
}
