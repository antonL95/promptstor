package codex

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
	configMode       = fs.FileMode(0o600)
	reasonNoScoped   = "Codex has no scoped rules"
	reasonAgentsOnly = "Codex reads only AGENTS.md in a project"
	reasonNoFileName = "the name has no characters usable in a file name"
)

type exporter struct {
	t           adapter.Target
	l           layout
	units       []plan.Unit
	unsupported []adapter.Unsupported
}

func (Adapter) Export(t adapter.Target, items []library.Item) ([]plan.Unit, []adapter.Unsupported, error) {
	l, ok := layoutFor(t)
	if !ok {
		return nil, nil, nil
	}
	ex := &exporter{t: t, l: l}
	ex.rules(items)
	var permissions, servers []library.Item
	for _, it := range items {
		switch it.Kind {
		case library.KindSkill:
			ex.skill(it)
		case library.KindCommand:
			ex.command(it)
		case library.KindSubagent:
			ex.subagent(it)
		case library.KindPermission:
			permissions = append(permissions, it)
		case library.KindHook:
			ex.hook(it)
		case library.KindMCP:
			servers = append(servers, it)
		}
	}
	if err := ex.permissions(permissions); err != nil {
		return nil, nil, err
	}
	if err := ex.servers(servers); err != nil {
		return nil, nil, err
	}
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
	if u.Root == "" {
		u.Root = ex.l.root
	}
	if u.Mode == 0 {
		u.Mode = fileMode
	}
	ex.units = append(ex.units, u)
}

func (ex *exporter) rules(items []library.Item) {
	if ex.l.scope == adapter.ScopeProject {
		_, perTool := agentsmd.Partition(items, ex.t.Selected)
		for _, it := range perTool[tool] {
			ex.report(it, reasonAgentsOnly)
		}
		return
	}
	var always []library.Item
	var ids []library.ID
	for _, it := range items {
		switch {
		case it.Kind != library.KindRule:
		case it.Activation == library.ActivationFiles || it.Activation == library.ActivationAgent || len(it.Scope) > 0:
			ex.report(it, reasonNoScoped)
		default:
			always = append(always, it)
			ids = append(ids, it.ID)
		}
	}
	if content := mdrules.Render(always); len(content) > 0 {
		ex.add(plan.Unit{Path: ex.l.instructions, Kind: manifest.UnitSection, Content: content, Items: ids})
	}
}
