package antigravity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	hookPrefix         = "promptstor-"
	enabledField       = "enabled"
	handlersField      = "hooks"
	commandField       = "command"
	typeField          = "type"
	commandHandler     = "command"
	reasonNoNative     = "no antigravity native entry"
	reasonUserHooks    = "PromptStor writes Antigravity hooks at user scope only"
	reasonHookShape    = "the antigravity hook is not a definition Antigravity knows"
	reasonHookPresent  = "hooks.json already has this hook as %q; remove it there to let PromptStor manage it"
	reasonHookNoName   = "the name has no characters usable in a hook name"
	reasonNoHookTarget = "the antigravity hook has no event"
)

var (
	groupedEvents = []string{"PreToolUse", "PostToolUse"}
	flatEvents    = []string{"SessionStart", "PreInvocation", "PostInvocation", "Stop"}
)

func (ex *exporter) hook(it library.Item, existing map[string]json.RawMessage) {
	definition, ok := it.Native[tool].(map[string]any)
	if !ok {
		ex.report(it, reasonNoNative)
		return
	}
	slug := library.Slug(it.Name)
	switch {
	case ex.l.scope != adapter.ScopeUser:
		ex.report(it, reasonUserHooks)
		return
	case slug == "":
		ex.report(it, reasonHookNoName)
		return
	}
	if reason := checkHook(definition); reason != "" {
		ex.report(it, reason)
		return
	}
	content, err := marshal(definition)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	if name, present := sameUserHook(existing, content); present {
		ex.report(it, fmt.Sprintf(reasonHookPresent, name))
		return
	}
	ex.add(plan.Unit{Path: ex.l.in(hooksName), Kind: manifest.UnitJSON, Key: "/" + hookPrefix + slug, Content: content, Items: []library.ID{it.ID}})
}

func checkHook(definition map[string]any) string {
	events := 0
	for key, value := range definition {
		switch {
		case key == enabledField:
			if !isBool(value) {
				return reasonHookShape
			}
		case slices.Contains(groupedEvents, key):
			if !eachObject(value, isGroup) {
				return reasonHookShape
			}
			events++
		case slices.Contains(flatEvents, key):
			if !eachObject(value, isHandler) {
				return reasonHookShape
			}
			events++
		default:
			return reasonHookShape
		}
	}
	if events == 0 {
		return reasonNoHookTarget
	}
	return ""
}

func eachObject(value any, accepts func(map[string]any) bool) bool {
	entries, ok := value.([]any)
	return ok && len(entries) > 0 && !slices.ContainsFunc(entries, func(entry any) bool {
		fields, isObject := entry.(map[string]any)
		return !isObject || !accepts(fields)
	})
}

func isGroup(fields map[string]any) bool {
	return eachObject(fields[handlersField], isHandler)
}

func isHandler(fields map[string]any) bool {
	command, _ := fields[commandField].(string)
	kind, typed := fields[typeField]
	return command != "" && (!typed || kind == commandHandler)
}

func sameUserHook(existing map[string]json.RawMessage, content []byte) (string, bool) {
	want, ok := canonical(content)
	if !ok {
		return "", false
	}
	for _, name := range slices.Sorted(maps.Keys(existing)) {
		if strings.HasPrefix(name, hookPrefix) {
			continue
		}
		if have, ok := canonical(existing[name]); ok && bytes.Equal(have, want) {
			return name, true
		}
	}
	return "", false
}

func canonical(raw []byte) ([]byte, bool) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, false
	}
	out, err := marshal(value)
	return out, err == nil
}
