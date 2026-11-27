package doctor

import (
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/plan"
)

func (d *doctor) snapshotEntries(found []detected) []plan.SnapshotEntry {
	tools := installedTools(found)
	var entries []plan.SnapshotEntry
	for _, t := range d.targets(tools) {
		for _, f := range found {
			if f.installed {
				entries = mergeEntries(entries, f.a.BackupSet(t))
			}
		}
		if t.Scope == adapter.ScopeProject {
			if a, ok := d.adapter(adapter.AgentsMD); ok {
				entries = mergeEntries(entries, a.BackupSet(t))
			}
		}
	}
	return entries
}

func (d *doctor) takeEntries(entries []plan.SnapshotEntry) (string, error) {
	snap, err := d.e.Backups().Take(backup.ReasonFirstRun, entries)
	if err != nil {
		return "", err
	}
	return snap.ID, nil
}

func (d *doctor) changedFileEntries(s *firstRunState) []plan.SnapshotEntry {
	var entries []plan.SnapshotEntry
	for _, m := range s.moves {
		if m.accepted {
			for _, r := range m.rules {
				entries = mergeEntries(entries, []plan.SnapshotEntry{{Tool: r.owner.tool, Path: m.path, Root: m.root}})
			}
		}
	}
	for _, del := range s.adoption.deletions {
		for _, c := range del.owners {
			entries = mergeEntries(entries, []plan.SnapshotEntry{{Tool: c.tool, Path: del.path, Root: del.root}})
		}
	}
	return entries
}

func mergeEntries(entries, more []plan.SnapshotEntry) []plan.SnapshotEntry {
	for _, entry := range more {
		at := slices.IndexFunc(entries, func(e plan.SnapshotEntry) bool { return e.Path == entry.Path })
		if at < 0 {
			entries = append(entries, entry)
			continue
		}
		merged := &entries[at]
		tools := strings.Split(merged.Tool, ",")
		if !slices.Contains(tools, entry.Tool) {
			merged.Tool = strings.Join(append(tools, entry.Tool), ",")
		}
		if len(merged.Keys) == 0 || len(entry.Keys) == 0 {
			merged.Keys = nil
		} else {
			merged.Keys = union(merged.Keys, entry.Keys)
		}
		merged.RedactKeys = union(merged.RedactKeys, entry.RedactKeys)
		if merged.Root == "" {
			merged.Root = entry.Root
		}
	}
	return entries
}

func union(a, b []string) []string {
	out := slices.Concat(a, b)
	slices.Sort(out)
	return slices.Compact(out)
}

func (d *doctor) targets(selected []string) []adapter.Target {
	targets := []adapter.Target{{Env: d.e.Env, Scope: adapter.ScopeUser, Selected: selected}}
	if d.opt.Project != "" {
		targets = append(targets, adapter.Target{Env: d.e.Env, Scope: adapter.ScopeProject, ProjectRoot: d.opt.Project, Selected: selected})
	}
	return targets
}

func (d *doctor) adapter(tool string) (adapter.Adapter, bool) {
	at := slices.IndexFunc(d.e.Adapters, func(a adapter.Adapter) bool { return a.ID() == tool })
	if at < 0 {
		return nil, false
	}
	return d.e.Adapters[at], true
}
