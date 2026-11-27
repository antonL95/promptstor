package cursor

import (
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
	hooksKey          = "hooks"
	hooksPointer      = "/hooks/"
	hookCommandKey    = "/command"
	versionKey        = "version"
	versionPointer    = "/version"
	hooksVersion      = "1"
	eventField        = "event"
	matcherField      = "matcher"
	commandHandler    = "command"
	reasonNoHook      = "no cursor native entry"
	reasonHookEvent   = "the cursor hook has no event that Cursor knows"
	reasonHookCommand = "Cursor hooks are keyed by command, and this hook has none"
	reasonHookType    = "PromptStor writes command hooks only"
	reasonHookField   = "hook field %s is not documented for Cursor"
	reasonHookTaken   = "another hook already uses this command for %s"
	reasonHookVersion = "hooks.json is not schema version 1; not written"
)

var (
	hookEvents = []string{
		"sessionStart", "sessionEnd", "preToolUse", "postToolUse", "postToolUseFailure", "subagentStart",
		"subagentStop", "beforeShellExecution", "afterShellExecution", "beforeMCPExecution",
		"afterMCPExecution", "beforeReadFile", "afterFileEdit", "beforeSubmitPrompt", "preCompact", "stop",
		"afterAgentResponse", "afterAgentThought", "beforeTabFileRead", "afterTabFileEdit", "workspaceOpen",
	}
	hookFields = []string{"command", "type", "timeout", "matcher", "failClosed", "loop_limit"}
)

type hookSet struct {
	otherVersion bool
	taken        map[string]bool
	items        []library.ID
}

func newHookSet(existing map[string]json.RawMessage) *hookSet {
	version, present := existing[versionKey]
	return &hookSet{otherVersion: present && string(version) != hooksVersion, taken: map[string]bool{}}
}

func (ex *exporter) hook(it library.Item, set *hookSet) {
	native, ok := it.Native[tool].(map[string]any)
	if !ok {
		ex.report(it, reasonNoHook)
		return
	}
	event, _ := native[eventField].(string)
	body := maps.Clone(native)
	delete(body, eventField)
	command, reason := checkHook(event, body)
	if reason != "" {
		ex.report(it, reason)
		return
	}
	key, err := adapter.ElementKey(hooksPointer+event, hookCommandKey, command)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	switch {
	case set.otherVersion:
		ex.report(it, reasonHookVersion)
		return
	case set.taken[key]:
		ex.report(it, fmt.Sprintf(reasonHookTaken, event))
		return
	}
	content, err := marshal(body)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	set.taken[key] = true
	set.items = append(set.items, it.ID)
	ex.add(plan.Unit{Path: ex.l.in(hooksName), Kind: manifest.UnitJSONElement, Key: key, Content: content, Items: []library.ID{it.ID}})
}

func checkHook(event string, body map[string]any) (command, reason string) {
	if !slices.Contains(hookEvents, event) {
		return "", reasonHookEvent
	}
	if kind, typed := body[typeField]; typed && kind != commandHandler {
		return "", reasonHookType
	}
	for _, field := range slices.Sorted(maps.Keys(body)) {
		if !slices.Contains(hookFields, field) {
			return "", fmt.Sprintf(reasonHookField, field)
		}
	}
	command, _ = body[commandField].(string)
	if command == "" {
		return "", reasonHookCommand
	}
	return command, ""
}

func (ex *exporter) hooksVersion(set *hookSet) {
	if len(set.items) == 0 {
		return
	}
	ex.add(plan.Unit{Path: ex.l.in(hooksName), Kind: manifest.UnitJSON, Key: versionPointer, Content: []byte(hooksVersion), Items: set.items})
}
