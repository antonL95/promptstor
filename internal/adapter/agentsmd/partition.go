package agentsmd

import (
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
)

func NeededFor(selected []adapter.ToolID) bool {
	return len(projectTools(selected)) > 0
}

func Partition(items []library.Item, selected []adapter.ToolID) (shared []library.Item, perTool map[adapter.ToolID][]library.Item) {
	tools := projectTools(selected)
	perTool = map[adapter.ToolID][]library.Item{}
	for _, it := range items {
		if it.Kind != library.KindRule {
			continue
		}
		targeted := targetedTools(it, tools)
		activation := activationOf(it)
		targetsAll := len(targeted) == len(tools)
		if targetsAll && (activation == library.ActivationAlways || !allSupport(targeted, activation)) {
			shared = append(shared, it)
			continue
		}
		for _, tool := range targeted {
			perTool[tool] = append(perTool[tool], it)
		}
	}
	return shared, perTool
}

func projectTools(selected []adapter.ToolID) []adapter.ToolID {
	tools := slices.DeleteFunc(slices.Clone(selected), func(tool adapter.ToolID) bool { return tool == adapter.AgentsMD })
	slices.Sort(tools)
	return slices.Compact(tools)
}

func targetedTools(it library.Item, tools []adapter.ToolID) []adapter.ToolID {
	merged := library.Merged{Items: []library.Item{it}}
	return slices.DeleteFunc(slices.Clone(tools), func(tool adapter.ToolID) bool { return len(merged.ForTool(tool)) == 0 })
}

func activationOf(it library.Item) library.Activation {
	switch {
	case it.Activation == library.ActivationAgent:
		return library.ActivationAgent
	case it.Activation == library.ActivationFiles || len(it.Scope) > 0:
		return library.ActivationFiles
	default:
		return library.ActivationAlways
	}
}

func supports(tool adapter.ToolID, activation library.Activation) bool {
	switch activation {
	case library.ActivationFiles:
		return tool == adapter.ClaudeCode || tool == adapter.Cursor || tool == adapter.Antigravity
	case library.ActivationAgent:
		return tool == adapter.Cursor || tool == adapter.Antigravity
	default:
		return true
	}
}

func allSupport(tools []adapter.ToolID, activation library.Activation) bool {
	return !slices.ContainsFunc(tools, func(tool adapter.ToolID) bool { return !supports(tool, activation) })
}
