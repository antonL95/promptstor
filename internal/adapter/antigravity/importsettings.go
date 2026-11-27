package antigravity

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/tailscale/hujson"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
)

const (
	permissionName     = "Antigravity permissions"
	noteInvalidJSON    = "invalid JSON; not imported"
	noteDenyEntry      = "permissions.deny entry %q is not portable; not imported"
	noteAskList        = "permissions.ask is not imported"
	noteHookDefinition = "hook %s is not an object; not imported"
	noteEntryKey       = "permissions.allow entry %q: %v"
)

func decodeObject(data []byte) (map[string]json.RawMessage, bool) {
	standard, err := hujson.Standardize(slices.Clone(data))
	var fields map[string]json.RawMessage
	if err != nil || json.Unmarshal(standard, &fields) != nil {
		return nil, false
	}
	return fields, true
}

func (im *importer) object(path string) (map[string]json.RawMessage, error) {
	data, found, err := im.read(path)
	if err != nil || !found {
		return nil, err
	}
	fields, valid := decodeObject(data)
	if !valid {
		im.note(path, noteInvalidJSON)
	}
	return fields, nil
}

func textList(raw json.RawMessage) []string {
	var entries []any
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	var found []string
	for _, entry := range entries {
		if text, ok := entry.(string); ok {
			found = append(found, text)
		}
	}
	return found
}

func (im *importer) permissions() error {
	if im.l.scope != adapter.ScopeUser {
		return nil
	}
	path := im.l.cliSettings()
	settings, err := im.object(path)
	if err != nil {
		return err
	}
	lists := rawObject(settings[permissionsKey])
	for _, entry := range textList(lists["allow"]) {
		key, err := adapter.ElementKey(allowPointer, "", entry)
		if err != nil {
			im.note(path, noteEntryKey, entry, err)
			continue
		}
		im.out.Grants = append(im.out.Grants, adapter.Grant{Tool: tool, Path: path, Key: key, Value: entry})
	}
	if len(textList(lists["ask"])) > 0 {
		im.note(path, noteAskList)
	}
	im.denies(path, textList(lists["deny"]))
	return nil
}

func (im *importer) denies(path string, rules []string) {
	portable := &library.PortablePermission{}
	for _, rule := range rules {
		action, target := splitRule(rule)
		switch action {
		case commandAction:
			if prefix, ok := commandPrefix(target); ok && prefix == target {
				portable.DenyCommands = append(portable.DenyCommands, target)
				continue
			}
		case readAction:
			if denied, ok := im.portablePath(target); ok {
				portable.DenyPaths = append(portable.DenyPaths, denied)
				continue
			}
		case writeAction:
			if _, ok := im.portablePath(target); ok && slices.Contains(rules, readAction+"("+target+")") {
				continue
			}
		}
		im.note(path, noteDenyEntry, rule)
	}
	if len(portable.DenyCommands)+len(portable.DenyPaths) > 0 {
		im.out.Items = append(im.out.Items, library.Item{Kind: library.KindPermission, Name: permissionName, Source: path, Portable: portable})
	}
}

func splitRule(rule string) (action, target string) {
	action, rest, found := strings.Cut(rule, "(")
	target, closed := strings.CutSuffix(rest, ")")
	if !found || !closed {
		return "", ""
	}
	return action, target
}

func (im *importer) portablePath(target string) (string, bool) {
	if native, ok := pathTarget(target, im.t.Env.Home); !ok || (native != target && !strings.HasPrefix(target, homePrefix)) {
		return "", false
	}
	if rest, underHome := strings.CutPrefix(target, im.t.Env.Home+"/"); underHome {
		return homePrefix + rest, true
	}
	return target, true
}

func (im *importer) hooks() error {
	if im.l.scope != adapter.ScopeUser {
		return nil
	}
	path := im.l.in(hooksName)
	hooks, err := im.object(path)
	if err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(hooks)) {
		var definition map[string]any
		if json.Unmarshal(hooks[key], &definition) != nil || definition == nil {
			im.note(path, noteHookDefinition, key)
			continue
		}
		name := key
		if stripped, own := strings.CutPrefix(key, hookPrefix); own {
			if _, taken := hooks[stripped]; !taken {
				name = stripped
			}
		}
		im.out.Items = append(im.out.Items, library.Item{
			Kind:   library.KindHook,
			Name:   name,
			Source: path,
			Native: map[string]any{tool: definition},
		})
	}
	return nil
}
