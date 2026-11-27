package engine

import (
	"cmp"
	"maps"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/adapter/agentsmd"
	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/policy"
)

type desired struct {
	units       []plan.Unit
	unsupported []adapter.Unsupported
	keep        []manifest.Entry
	sharedState []string
	selected    map[string]bool
	roots       map[string]bool
}

func (e Engine) Targets(projectRoot string) ([]adapter.Target, error) {
	targets := []adapter.Target{{Env: e.Env, Scope: adapter.ScopeUser, Selected: sortedSet(e.Config.Tools)}}
	if projectRoot == "" {
		return targets, nil
	}
	tools := e.Config.Tools
	project, found, err := config.LoadProject(projectRoot)
	if err != nil {
		return nil, err
	}
	if found && len(project.Tools) > 0 {
		tools = project.Tools
	}
	selected := sortedSet(tools)
	if agentsmd.NeededFor(selected) {
		selected = sortedSet(append(selected, adapter.AgentsMD))
	}
	return append(targets, adapter.Target{Env: e.Env, Scope: adapter.ScopeProject, ProjectRoot: projectRoot, Selected: selected}), nil
}

func (e Engine) desired(projectRoot string, lib scopedLibrary, m manifest.Manifest) (desired, error) {
	targets, err := e.Targets(projectRoot)
	if err != nil {
		return desired{}, err
	}
	d := desired{selected: map[string]bool{}, roots: map[string]bool{e.Env.Home: true}, unsupported: slices.Clone(e.Notices)}
	var projectAcks policy.Acks
	if projectRoot != "" {
		d.roots[projectRoot] = true
		if projectAcks, err = policy.LoadProjectAcks(projectRoot); err != nil {
			return desired{}, err
		}
	}
	for _, t := range targets {
		merged := lib.scopes[t.Scope]
		scoped := e
		if t.Scope == adapter.ScopeProject {
			scoped.Acks = e.Acks.WithProject(projectAcks, merged.Items)
		}
		for _, tool := range t.Selected {
			a, err := e.adapterFor(tool)
			if err != nil {
				return desired{}, err
			}
			items := merged.Items
			if tool != adapter.AgentsMD {
				items = merged.ForTool(tool)
			}
			if t.Scope != adapter.ScopeUser {
				items = slices.DeleteFunc(slices.Clone(items), func(it library.Item) bool { return library.IsBuiltin(it.ID) })
			}
			result, err := scoped.export(t, a, items, m)
			if err != nil {
				return desired{}, err
			}
			d.add(t, a, result)
		}
	}
	return d, nil
}

func (d *desired) add(t adapter.Target, a adapter.Adapter, result exported) {
	d.selected[a.ID()] = true
	d.units = append(d.units, result.units...)
	d.unsupported = append(d.unsupported, result.unsupported...)
	d.keep = append(d.keep, result.keep...)
	for _, u := range result.units {
		d.roots[u.Root] = true
	}
	for _, entry := range a.BackupSet(t) {
		if entry.Root != "" {
			d.roots[entry.Root] = true
		}
		if len(entry.Keys) > 0 && !slices.Contains(d.sharedState, entry.Path) {
			d.sharedState = append(d.sharedState, entry.Path)
		}
	}
}

func (d desired) tools() []string {
	return slices.Sorted(maps.Keys(d.selected))
}

func (d desired) outOfScope(m manifest.Manifest) []manifest.Entry {
	var keep []manifest.Entry
	for _, entry := range m.Entries {
		if !d.roots[entry.Root] {
			keep = append(keep, entry)
		}
	}
	return keep
}

func (d desired) report() []adapter.Unsupported {
	report := slices.Clone(d.unsupported)
	slices.SortFunc(report, compareUnsupported)
	return slices.Compact(report)
}

func compareUnsupported(a, b adapter.Unsupported) int {
	return cmp.Or(
		cmp.Compare(a.Tool, b.Tool),
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.ItemID, b.ItemID),
		cmp.Compare(a.Reason, b.Reason),
	)
}

func sortedSet(values []string) []string {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}
