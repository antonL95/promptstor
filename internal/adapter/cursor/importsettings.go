package cursor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
)

const (
	permissionName = "Cursor permissions"
	noteDenyEntry  = "permissions.deny entry %q is not portable; not imported"
	noteEntryKey   = "permissions.allow entry %q: %v"
	noteCLIMoved   = "the Cursor CLI may read cli-config.json from another folder here; not imported"
	noteVersion    = "not schema version 1; hooks not imported"
	noteHookShape  = "hooks.%s is not a list of hook definitions; not imported"
	noteHook       = "hook in %s: %s; not imported"
)

func (im *importer) permissions() error {
	path := im.l.cliConfig()
	if cliConfigMoved(im.t) {
		if exists(path) {
			im.note(path, noteCLIMoved)
		}
		return nil
	}
	config, err := im.object(path)
	if err != nil {
		return err
	}
	lists := rawObject(config[permissionsKey])
	for _, entry := range textList(lists["allow"]) {
		key, err := adapter.ElementKey(allowPointer, "", entry)
		if err != nil {
			im.note(path, noteEntryKey, entry, err)
			continue
		}
		im.out.Grants = append(im.out.Grants, adapter.Grant{Tool: tool, Path: path, Key: key, Value: entry})
	}
	im.denies(path, textList(lists["deny"]))
	return nil
}

func (im *importer) denies(path string, rules []string) {
	portable := &library.PortablePermission{}
	for _, rule := range rules {
		action, target := splitRule(rule)
		switch action {
		case shellAction:
			if base, ok := commandBase(target); ok && base == target {
				portable.DenyCommands = append(portable.DenyCommands, target)
				continue
			}
		case readAction:
			if denied, ok := im.portablePath(target); ok && slices.Contains(rules, writeAction+"("+target+")") {
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
	if !expressiblePath(target) || strings.HasPrefix(target, homeShorthand) {
		return "", false
	}
	if rest, underHome := strings.CutPrefix(target, im.t.Env.Home+"/"); underHome && im.l.scope == adapter.ScopeUser {
		return homePrefix + rest, true
	}
	return target, true
}

func (im *importer) hooks() error {
	path := im.l.in(hooksName)
	config, err := im.object(path)
	if err != nil || config == nil {
		return err
	}
	if string(config[versionKey]) != hooksVersion {
		im.note(path, noteVersion)
		return nil
	}
	events := rawObject(config[hooksKey])
	used := map[string]int{}
	for _, event := range slices.Sorted(maps.Keys(events)) {
		var definitions []map[string]any
		decoder := json.NewDecoder(bytes.NewReader(events[event]))
		decoder.UseNumber()
		if decoder.Decode(&definitions) != nil {
			im.note(path, noteHookShape, event)
			continue
		}
		for _, definition := range definitions {
			im.hookItem(path, event, definition, used)
		}
	}
	return nil
}

func (im *importer) hookItem(path, event string, definition map[string]any, used map[string]int) {
	native := map[string]any{}
	for key, value := range definition {
		native[key] = plain(value)
	}
	if _, reason := checkHook(event, native); reason != "" {
		im.note(path, noteHook, event, reason)
		return
	}
	native[eventField] = event
	name := event
	if matcher, _ := native[matcherField].(string); matcher != "" {
		name += " " + matcher
	}
	if used[name]++; used[name] > 1 {
		name = fmt.Sprintf("%s %d", name, used[name])
	}
	im.out.Items = append(im.out.Items, library.Item{Kind: library.KindHook, Name: name, Source: path, Native: map[string]any{tool: native}})
}

func plain(value any) any {
	number, ok := value.(json.Number)
	if !ok {
		return value
	}
	if whole, err := number.Int64(); err == nil {
		return int(whole)
	}
	fraction, _ := number.Float64()
	return fraction
}
