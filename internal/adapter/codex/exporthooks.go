package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	hooksPointer        = "/hooks/"
	hookCommandPointer  = "/hooks/0/command"
	reasonNoNative      = "no codex native entry"
	reasonHookEvent     = "the codex hook has no event that Codex knows"
	reasonNoHookCommand = "Codex hooks are keyed by the command of their first handler, and this hook has none"
	noteGuardTrust      = "Codex runs the edit guard only after you trust it in /hooks"
)

var hookEvents = []string{
	"SessionStart", "SessionEnd", "SubagentStart", "PreToolUse", "PermissionRequest", "PostToolUse",
	"PreModelRequest", "PostModelRequest", "PreCompact", "PostCompact", "UserPromptSubmit",
	"SubagentStop", "Stop", "Interrupt",
}

func (ex *exporter) hook(it library.Item) {
	group, ok := it.Native[tool].(map[string]any)
	if !ok {
		ex.report(it, reasonNoNative)
		return
	}
	event, _ := group[eventField].(string)
	if !slices.Contains(hookEvents, event) {
		ex.report(it, reasonHookEvent)
		return
	}
	command, ok := firstCommand(group)
	if !ok {
		ex.report(it, reasonNoHookCommand)
		return
	}
	key, err := adapter.ElementKey(hooksPointer+event, hookCommandPointer, command)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	body := maps.Clone(group)
	delete(body, eventField)
	content, err := marshalJSON(body)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	ex.add(plan.Unit{Path: ex.l.in(hooksName), Kind: manifest.UnitJSONElement, Key: key, Content: content, Items: []library.ID{it.ID}})
	if library.IsEditGuard(it) {
		ex.report(it, noteGuardTrust)
	}
}

func firstCommand(group map[string]any) (string, bool) {
	handlers, ok := group[hooksKey].([]any)
	if !ok || len(handlers) == 0 {
		return "", false
	}
	handler, ok := handlers[0].(map[string]any)
	if !ok {
		return "", false
	}
	command, ok := handler[fieldCommand].(string)
	return command, ok && command != ""
}

func marshalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}
