package agentsmd_test

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/adapter/agentsmd"
	"github.com/antonL95/promptstor/internal/library"
)

func ids(items []library.Item) []library.ID {
	var found []library.ID
	for _, it := range items {
		found = append(found, it.ID)
	}
	return found
}

func TestPartitionSharedVsTargeted(t *testing.T) {
	always := library.Item{ID: "always", Kind: library.KindRule, Activation: library.ActivationAlways}
	plain := library.Item{ID: "plain", Kind: library.KindRule}
	claudeOnly := library.Item{ID: "claude-only", Kind: library.KindRule, Targets: library.Targets{Include: []string{adapter.ClaudeCode}}}
	notCodex := library.Item{ID: "not-codex", Kind: library.KindRule, Targets: library.Targets{Exclude: []string{adapter.Codex}}}
	unselected := library.Item{ID: "unselected", Kind: library.KindRule, Targets: library.Targets{Include: []string{adapter.OpenCode}}}
	files := library.Item{ID: "files", Kind: library.KindRule, Activation: library.ActivationFiles, Scope: []string{"*.go"}}
	globsOnly := library.Item{ID: "globs-only", Kind: library.KindRule, Scope: []string{"*.go"}}
	agent := library.Item{ID: "agent", Kind: library.KindRule, Activation: library.ActivationAgent}
	scopedAgent := library.Item{ID: "scoped-agent", Kind: library.KindRule, Activation: library.ActivationAgent, Scope: []string{"*.go"}}
	cursorFiles := library.Item{ID: "cursor-files", Kind: library.KindRule, Activation: library.ActivationFiles, Scope: []string{"*.go"}, Targets: library.Targets{Include: []string{adapter.Cursor}}}
	skill := library.Item{ID: "skill", Kind: library.KindSkill}
	all := []library.Item{unselected, files, always, claudeOnly, skill, agent, plain, notCodex, globsOnly, scopedAgent, cursorFiles}

	cases := []struct {
		name     string
		selected []adapter.ToolID
		shared   []library.ID
		perTool  map[adapter.ToolID][]library.ID
	}{
		{
			"codex lacks scoping",
			[]adapter.ToolID{adapter.ClaudeCode, adapter.Codex, adapter.AgentsMD},
			[]library.ID{"files", "always", "agent", "plain", "globs-only", "scoped-agent"},
			map[adapter.ToolID][]library.ID{
				adapter.ClaudeCode: {"claude-only", "not-codex"},
			},
		},
		{
			"claude and cursor scope files, only cursor activates by agent",
			[]adapter.ToolID{adapter.ClaudeCode, adapter.Cursor},
			[]library.ID{"always", "agent", "plain", "not-codex", "scoped-agent"},
			map[adapter.ToolID][]library.ID{
				adapter.ClaudeCode: {"files", "claude-only", "globs-only"},
				adapter.Cursor:     {"files", "globs-only", "cursor-files"},
			},
		},
		{
			"cursor and antigravity support both",
			[]adapter.ToolID{adapter.Antigravity, adapter.Cursor, adapter.Cursor},
			[]library.ID{"always", "plain", "not-codex"},
			map[adapter.ToolID][]library.ID{
				adapter.Antigravity: {"files", "agent", "globs-only", "scoped-agent"},
				adapter.Cursor:      {"files", "agent", "globs-only", "scoped-agent", "cursor-files"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shared, perTool := agentsmd.Partition(all, c.selected)
			if got := ids(shared); !reflect.DeepEqual(got, c.shared) {
				t.Errorf("shared %v, want %v", got, c.shared)
			}
			got := map[adapter.ToolID][]library.ID{}
			for tool, items := range perTool {
				got[tool] = ids(items)
			}
			if !reflect.DeepEqual(got, c.perTool) {
				t.Errorf("per tool %v (tools %v), want %v", got, slices.Sorted(maps.Keys(got)), c.perTool)
			}
		})
	}
}

func TestNeededFor(t *testing.T) {
	cases := []struct {
		selected []adapter.ToolID
		want     bool
	}{
		{nil, false},
		{[]adapter.ToolID{adapter.AgentsMD}, false},
		{[]adapter.ToolID{adapter.ClaudeCode}, true},
		{[]adapter.ToolID{adapter.GeminiCLI}, true},
		{[]adapter.ToolID{adapter.AgentsMD, adapter.Codex}, true},
	}
	for _, c := range cases {
		if got := agentsmd.NeededFor(c.selected); got != c.want {
			t.Errorf("NeededFor(%v) = %v", c.selected, got)
		}
	}
}

func TestPartitionSubsetScopedRuleNeverShared(t *testing.T) {
	selected := []adapter.ToolID{adapter.ClaudeCode, adapter.Codex, adapter.Cursor, adapter.GeminiCLI, adapter.OpenCode, adapter.AgentsMD}
	cases := []struct {
		name    string
		targets []adapter.ToolID
	}{
		{"one tool without a rule file", []adapter.ToolID{adapter.Codex}},
		{"native and not native", []adapter.ToolID{adapter.ClaudeCode, adapter.OpenCode}},
		{"none native", []adapter.ToolID{adapter.GeminiCLI, adapter.OpenCode}},
		{"all native", []adapter.ToolID{adapter.Cursor}},
	}
	activations := map[string]library.Item{
		"always": {},
		"files":  {Activation: library.ActivationFiles, Scope: []string{"*.go"}},
		"globs":  {Scope: []string{"*.go"}},
		"agent":  {Activation: library.ActivationAgent, Description: "Releasing"},
	}
	for _, c := range cases {
		for _, activation := range slices.Sorted(maps.Keys(activations)) {
			t.Run(c.name+" "+activation, func(t *testing.T) {
				rule := activations[activation]
				rule.ID, rule.Kind, rule.Body = "subset", library.KindRule, "## Subset\n"
				rule.Targets = library.Targets{Include: c.targets}
				shared, perTool := agentsmd.Partition([]library.Item{rule}, selected)
				if len(shared) != 0 {
					t.Errorf("shared %v", ids(shared))
				}
				if got := slices.Sorted(maps.Keys(perTool)); !reflect.DeepEqual(got, c.targets) {
					t.Errorf("per tool %v, want %v", got, c.targets)
				}
				for tool, items := range perTool {
					if !reflect.DeepEqual(ids(items), []library.ID{"subset"}) {
						t.Errorf("%s gets %v", tool, ids(items))
					}
				}
				tg := target(t, adapter.ScopeProject, selected...)
				if units, unsupported := export(t, tg, []library.Item{rule}); len(units) != 0 || len(unsupported) != 0 {
					t.Errorf("units %+v, unsupported %+v", units, unsupported)
				}
			})
		}
	}
}
