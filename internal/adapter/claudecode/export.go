package claudecode

import (
	"cmp"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/adapter/agentsmd"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/mdrules"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	agentsImport     = "@" + agentsName + "\n"
	reasonUseSkill   = "use a skill"
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
	for _, it := range items {
		switch it.Kind {
		case library.KindSkill:
			ex.skill(it)
		case library.KindCommand:
			ex.command(it)
		case library.KindSubagent:
			ex.subagent(it)
		case library.KindPermission:
			ex.permission(it)
		case library.KindHook:
			ex.hook(it)
		case library.KindMCP:
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

func (ex *exporter) add(u plan.Unit) {
	u.Tool = tool
	if u.Root == "" {
		u.Root = ex.l.root
	}
	ex.units = append(ex.units, u)
}

func (ex *exporter) rules(items []library.Item) {
	rules := items
	if ex.l.scope == adapter.ScopeProject {
		shared, perTool := agentsmd.Partition(items, ex.t.Selected)
		rules = perTool[tool]
		always := ex.ruleFiles(rules)
		if len(shared) == 0 && len(always) == 0 {
			return
		}
		body := agentsImport
		if len(always) > 0 {
			body += "\n" + string(mdrules.Render(always))
		}
		ex.section(always, body)
		return
	}
	var onlyRules []library.Item
	for _, it := range rules {
		if it.Kind == library.KindRule {
			onlyRules = append(onlyRules, it)
		}
	}
	if always := ex.ruleFiles(onlyRules); len(always) > 0 {
		ex.section(always, string(mdrules.Render(always)))
	}
}

func (ex *exporter) ruleFiles(rules []library.Item) []library.Item {
	var always []library.Item
	for _, it := range rules {
		switch {
		case it.Activation == library.ActivationAgent:
			ex.report(it, reasonUseSkill)
		case len(it.Scope) > 0:
			ex.ruleFile(it)
		default:
			always = append(always, it)
		}
	}
	return always
}

func (ex *exporter) section(rules []library.Item, body string) {
	var ids []library.ID
	for _, it := range rules {
		ids = append(ids, it.ID)
	}
	ex.add(plan.Unit{Path: ex.l.instructions, Kind: manifest.UnitSection, Content: []byte(body), Mode: fileMode, Items: ids})
}
