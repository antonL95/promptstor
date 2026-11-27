package library

import (
	"regexp"
	"slices"
	"strings"
)

const (
	editGuardPrefix  = "ps_builtin_guard_"
	editGuardName    = "edit guard"
	opencodeTool     = "opencode"
	eventKey         = "event"
	hookCommand      = "command"
	hookHandlersKey  = "hooks"
	hookMatcherKey   = "matcher"
	hookTypeKey      = "type"
	preToolUse       = "PreToolUse"
	opencodeEdit     = "edit"
	opencodeDeny     = "deny"
	opencodeAction   = "action"
	opencodeResource = "resource"
	opencodeEffect   = "effect"
)

var (
	shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

	guardMatchers = map[string]string{
		"claude-code": "Edit|MultiEdit|Write|NotebookEdit",
		"codex":       "apply_patch",
		"gemini-cli":  "write_file|replace",
		"cursor":      "Write",
	}
	guardEvents = map[string]string{
		"claude-code": preToolUse,
		"codex":       preToolUse,
		"gemini-cli":  "BeforeTool",
		"cursor":      "preToolUse",
	}
)

func GuardedTools() []string {
	tools := make([]string, 0, len(guardMatchers))
	for tool := range guardMatchers {
		tools = append(tools, tool)
	}
	slices.Sort(tools)
	return tools
}

func IsEditGuard(it Item) bool {
	return strings.HasPrefix(string(it.ID), editGuardPrefix)
}

func EditGuard(tool, binary string) Item {
	command := shellQuote(binary) + " hook " + tool
	handler := map[string]any{hookTypeKey: hookCommand, hookCommand: command}
	matcher := guardMatchers[tool]
	var native map[string]any
	switch tool {
	case "cursor":
		native = map[string]any{eventKey: guardEvents[tool], hookMatcherKey: matcher, hookTypeKey: hookCommand, hookCommand: command}
	default:
		native = map[string]any{eventKey: guardEvents[tool], hookMatcherKey: matcher, hookHandlersKey: []any{handler}}
	}
	return Item{
		ID:          ID(editGuardPrefix + tool),
		Kind:        KindHook,
		Name:        editGuardName + " " + tool,
		Layer:       LayerCompany,
		Description: "Blocks direct edits of files PromptStor generates",
		Targets:     Targets{Include: []string{tool}},
		Native:      map[string]any{tool: native},
	}
}

func OpencodeGuard(paths []string) Item {
	rules := make([]any, 0, len(paths))
	for _, path := range slices.Sorted(slices.Values(paths)) {
		rules = append(rules, map[string]any{opencodeAction: opencodeEdit, opencodeResource: path, opencodeEffect: opencodeDeny})
	}
	return Item{
		ID:          ID(editGuardPrefix + opencodeTool),
		Kind:        KindPermission,
		Name:        editGuardName + " " + opencodeTool,
		Layer:       LayerCompany,
		Description: "Denies opencode edits of files PromptStor generates",
		Targets:     Targets{Include: []string{opencodeTool}},
		Native:      map[string]any{opencodeTool: rules},
	}
}

func shellQuote(value string) string {
	if shellSafe.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
