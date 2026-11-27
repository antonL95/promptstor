package geminicli

import (
	"cmp"
	"io/fs"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/adapter/agentsmd"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/mdrules"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	fileMode         = fs.FileMode(0o644)
	reasonNoScoped   = "Gemini CLI has no scoped rules"
	reasonAgentsOnly = "Gemini CLI reads only AGENTS.md and GEMINI.md in a project"
	reasonNoFileName = "the name has no characters usable in a file name"
)

type exporter struct {
	t           adapter.Target
	l           layout
	existing    map[string]any
	units       []plan.Unit
	unsupported []adapter.Unsupported
}

func (Adapter) Export(t adapter.Target, items []library.Item) ([]plan.Unit, []adapter.Unsupported, error) {
	l, ok := layoutFor(t)
	if !ok {
		return nil, nil, nil
	}
	existing, _, err := readSettings(l.settings)
	if err != nil {
		return nil, nil, err
	}
	ex := &exporter{t: t, l: l, existing: existing}
	if l.scope == adapter.ScopeUser {
		ex.userRules(items)
	} else {
		if err := ex.projectRules(items); err != nil {
			return nil, nil, err
		}
	}
	var policy policyFile
	for _, it := range items {
		switch it.Kind {
		case library.KindSkill:
			ex.skill(it)
		case library.KindCommand:
			ex.command(it)
		case library.KindSubagent:
			ex.subagent(it)
		case library.KindPermission:
			ex.permission(it, &policy)
		case library.KindHook:
			ex.hook(it)
		case library.KindMCP:
			ex.server(it)
		}
	}
	ex.policy(policy)
	slices.SortStableFunc(ex.units, func(a, b plan.Unit) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Key, b.Key))
	})
	return ex.units, ex.unsupported, nil
}

func (ex *exporter) report(it library.Item, reason string) {
	ex.unsupported = append(ex.unsupported, adapter.Unsupported{ItemID: it.ID, Name: it.Name, Tool: tool, Reason: reason})
}

func (ex *exporter) block(it library.Item, reason string) {
	ex.unsupported = append(ex.unsupported, adapter.Unsupported{ItemID: it.ID, Name: it.Name, Tool: tool, Reason: reason, Blocking: true})
}

func (ex *exporter) add(u plan.Unit) {
	u.Tool = tool
	u.Root = ex.l.root
	if u.Mode == 0 {
		u.Mode = fileMode
	}
	ex.units = append(ex.units, u)
}

func (ex *exporter) userRules(items []library.Item) {
	var always []library.Item
	for _, it := range items {
		switch {
		case it.Kind != library.KindRule:
		case it.Activation == library.ActivationFiles || it.Activation == library.ActivationAgent || len(it.Scope) > 0:
			ex.report(it, reasonNoScoped)
		default:
			always = append(always, it)
		}
	}
	content := mdrules.Render(always)
	if len(content) == 0 {
		return
	}
	memory, reason := ex.l.memoryFile(ex.t.Env)
	if reason != "" {
		for _, it := range always {
			ex.report(it, reason)
		}
		return
	}
	ex.add(plan.Unit{Path: memory, Kind: manifest.UnitSection, Content: content, Items: idsOf(always)})
}

func (ex *exporter) projectRules(items []library.Item) error {
	shared, perTool := agentsmd.Partition(items, ex.t.Selected)
	for _, it := range perTool[tool] {
		ex.report(it, reasonAgentsOnly)
	}
	if len(shared) == 0 {
		return nil
	}
	if userSetContextFiles(ex.existing) {
		for _, it := range shared {
			ex.block(it, reasonUserFileName)
		}
		return nil
	}
	content, err := marshal(projectContextFiles)
	if err != nil {
		return err
	}
	ex.add(plan.Unit{Path: ex.l.settings, Kind: manifest.UnitJSON, Key: contextPointer, Content: content, Items: idsOf(shared)})
	return nil
}

func idsOf(items []library.Item) []library.ID {
	ids := make([]library.ID, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}
