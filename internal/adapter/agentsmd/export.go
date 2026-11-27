package agentsmd

import (
	"io/fs"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/mdrules"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	fileMode  = fs.FileMode(0o644)
	rulesOnly = "AGENTS.md holds rules only"
)

var unenforced = map[library.Activation]string{
	library.ActivationFiles: "file scope not enforced; the rule applies everywhere",
	library.ActivationAgent: "agent activation not supported; the rule always applies",
}

func (agentsMD) Export(t adapter.Target, items []library.Item) ([]plan.Unit, []adapter.Unsupported, error) {
	path, ok := projectFile(t)
	if !ok {
		return nil, nil, nil
	}
	var unsupported []adapter.Unsupported
	for _, it := range items {
		if it.Kind != library.KindRule {
			unsupported = append(unsupported, report(it, adapter.AgentsMD, rulesOnly))
		}
	}
	shared, _ := Partition(items, t.Selected)
	var ids []library.ID
	for i, it := range shared {
		unsupported = append(unsupported, unenforcedBy(it, projectTools(t.Selected))...)
		shared[i] = annotated(it)
		ids = append(ids, it.ID)
	}
	content := mdrules.Render(shared)
	if len(content) == 0 {
		return nil, unsupported, nil
	}
	unit := plan.Unit{
		Tool:    adapter.AgentsMD,
		Path:    path,
		Root:    t.ProjectRoot,
		Kind:    manifest.UnitSection,
		Content: content,
		Mode:    fileMode,
		Items:   ids,
	}
	return []plan.Unit{unit}, unsupported, nil
}

func unenforcedBy(it library.Item, tools []adapter.ToolID) []adapter.Unsupported {
	activation := activationOf(it)
	var unsupported []adapter.Unsupported
	for _, tool := range tools {
		if !supports(tool, activation) {
			unsupported = append(unsupported, report(it, tool, unenforced[activation]))
		}
	}
	return unsupported
}

func report(it library.Item, tool adapter.ToolID, reason string) adapter.Unsupported {
	return adapter.Unsupported{ItemID: it.ID, Name: it.Name, Tool: tool, Reason: reason}
}
