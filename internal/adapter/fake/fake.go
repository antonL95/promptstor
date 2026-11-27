package fake

import (
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
)

type Adapter struct {
	Tool        adapter.ToolID
	Units       []plan.Unit
	Unsupported []adapter.Unsupported
	Caps        adapter.Capabilities
	Detection   adapter.Detection
	Imports     adapter.Imported
	Backup      []plan.SnapshotEntry
}

func (a *Adapter) ID() adapter.ToolID { return a.Tool }

func (a *Adapter) Detect(paths.Env) adapter.Detection { return a.Detection }

func (a *Adapter) Capabilities() adapter.Capabilities { return a.Caps }

func (a *Adapter) Import(adapter.Target) (adapter.Imported, error) { return a.Imports, nil }

func (a *Adapter) Export(adapter.Target, []library.Item) ([]plan.Unit, []adapter.Unsupported, error) {
	units := make([]plan.Unit, len(a.Units))
	for i, u := range a.Units {
		u.Content = slices.Clone(u.Content)
		u.Items = slices.Clone(u.Items)
		units[i] = u
	}
	return units, slices.Clone(a.Unsupported), nil
}

func (a *Adapter) BackupSet(adapter.Target) []plan.SnapshotEntry { return a.Backup }
