package opencode

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
	reasonNoScoped   = "opencode has no scoped rules"
	reasonAgentsOnly = "opencode reads only AGENTS.md in a project"
	reasonNoHooks    = "opencode has only JS plugins"
	reasonNoFileName = "the name has no characters usable in a file name"
)

type exporter struct {
	t           adapter.Target
	l           layout
	servers     map[string]mcpEntry
	nested      bool
	units       []plan.Unit
	unsupported []adapter.Unsupported
}

func (Adapter) Export(t adapter.Target, items []library.Item) ([]plan.Unit, []adapter.Unsupported, error) {
	l, ok := layoutFor(t)
	if !ok {
		return nil, nil, nil
	}
	documents, err := readDocuments(t.Env, l.configPaths(), func(string, string) {})
	if err != nil {
		return nil, nil, err
	}
	ex := &exporter{t: t, l: l, servers: mergedServers(documents)}
	ex.nested = hasServerNamedServers(l, documents)
	ex.rules(items)
	for _, it := range items {
		switch {
		case it.Kind == library.KindSkill:
			ex.skill(it)
		case it.Kind == library.KindHook:
			ex.report(it, reasonNoHooks)
		case it.Kind == library.KindRule:
		case l.unknownConfig:
			ex.report(it, reasonUnknownLocal)
		case it.Kind == library.KindCommand:
			ex.command(it)
		case it.Kind == library.KindSubagent:
			ex.subagent(it)
		case it.Kind == library.KindPermission:
			ex.permission(it)
		case it.Kind == library.KindMCP:
			ex.server(it)
		}
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
	u.Root = ex.l.root
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
		case ex.l.unknownConfig:
			ex.report(it, reasonUnknownLocal)
		case it.Activation == library.ActivationFiles || it.Activation == library.ActivationAgent || len(it.Scope) > 0:
			ex.report(it, reasonNoScoped)
		default:
			always = append(always, it)
			ids = append(ids, it.ID)
		}
	}
	if content := mdrules.Render(always); len(content) > 0 {
		ex.add(plan.Unit{Path: ex.l.at(instructionsName), Kind: manifest.UnitSection, Content: content, Items: ids})
	}
}
