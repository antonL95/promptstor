package agentsmd

import (
	"path/filepath"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
)

const fileName = "AGENTS.md"

type agentsMD struct{}

func init() {
	adapter.Register(agentsMD{})
}

func (agentsMD) ID() adapter.ToolID { return adapter.AgentsMD }

func (agentsMD) Detect(paths.Env) adapter.Detection { return adapter.Detection{} }

func (agentsMD) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Kinds: map[library.Kind]bool{library.KindRule: true}}
}

func (agentsMD) BackupSet(t adapter.Target) []plan.SnapshotEntry {
	path, ok := projectFile(t)
	if !ok {
		return nil
	}
	return []plan.SnapshotEntry{{Tool: adapter.AgentsMD, Path: path, Root: t.ProjectRoot}}
}

func projectFile(t adapter.Target) (string, bool) {
	if t.Scope != adapter.ScopeProject || t.ProjectRoot == "" {
		return "", false
	}
	return filepath.Join(t.ProjectRoot, fileName), true
}
