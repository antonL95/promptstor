package engine

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/antonL95/promptstor/internal/adapter"
	_ "github.com/antonL95/promptstor/internal/adapter/all"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/policy"
	"github.com/antonL95/promptstor/internal/secrets"
)

const (
	projectLayerDir = ".promptstor"
	lockWait        = 5 * time.Second
)

type Engine struct {
	Env      paths.Env
	Config   config.Config
	Adapters []adapter.Adapter
	Store    backup.Store
	Resolver secrets.Resolver
	Acks     policy.Acks
	Now      func() time.Time
	Builtins []library.Item
	Notices  []adapter.Unsupported
}

type Plan struct {
	Changes     []plan.FileChange
	Unsupported []adapter.Unsupported
	Violations  []library.Violation
	LibraryHash string
}

var scopeLayers = map[adapter.Scope][]library.Layer{
	adapter.ScopeUser:    {library.LayerCompany, library.LayerTeam, library.LayerPersonal, library.LayerDevice},
	adapter.ScopeProject: {library.LayerCompany, library.LayerTeam, library.LayerProject},
}

type scopedLibrary struct {
	full       library.Merged
	violations []library.Violation
	scopes     map[adapter.Scope]library.Merged
}

func (e Engine) Library(projectRoot string) (library.Merged, []library.Violation, error) {
	layers, err := e.layers(projectRoot)
	if err != nil {
		return library.Merged{}, nil, err
	}
	merged, violations := library.Merge(layers)
	return merged, violations, nil
}

func (e Engine) scoped(projectRoot string) (scopedLibrary, error) {
	layers, err := e.layers(projectRoot)
	if err != nil {
		return scopedLibrary{}, err
	}
	s := scopedLibrary{scopes: map[adapter.Scope]library.Merged{}}
	s.full, s.violations = library.Merge(layers)
	for scope, allowed := range scopeLayers {
		kept := slices.DeleteFunc(slices.Clone(layers), func(l library.LayerItems) bool { return !slices.Contains(allowed, l.Layer) })
		s.scopes[scope], _ = library.Merge(kept)
	}
	return s, nil
}

func (e Engine) layers(projectRoot string) ([]library.LayerItems, error) {
	sources := []struct {
		dir   string
		layer library.Layer
	}{
		{e.Env.LibraryDir(), library.LayerPersonal},
		{e.Env.DeviceDir(), library.LayerDevice},
	}
	if projectRoot != "" {
		sources = append(sources, struct {
			dir   string
			layer library.Layer
		}{filepath.Join(projectRoot, projectLayerDir), library.LayerProject})
	}
	var layers []library.LayerItems
	for _, source := range sources {
		items, err := library.LoadLayer(source.dir, source.layer, library.LoadOptions{})
		if err != nil {
			return nil, fmt.Errorf("load %s layer: %w", source.layer, err)
		}
		layers = append(layers, library.LayerItems{Layer: source.layer, Items: items})
	}
	return append(layers, builtinLayers(e.Builtins)...), nil
}

func builtinLayers(builtins []library.Item) []library.LayerItems {
	var layers []library.LayerItems
	for _, it := range builtins {
		layer := cmp.Or(it.Layer, library.LayerCompany)
		at := slices.IndexFunc(layers, func(l library.LayerItems) bool { return l.Layer == layer })
		if at < 0 {
			layers = append(layers, library.LayerItems{Layer: layer})
			at = len(layers) - 1
		}
		layers[at].Items = append(layers[at].Items, it)
	}
	return layers
}

func (e Engine) Plan(projectRoot string, opt plan.Options) (Plan, error) {
	p, _, err := e.plan(projectRoot, opt)
	return p, err
}

func (e Engine) Sync(projectRoot string, opt plan.Options) (plan.ApplyResult, Plan, error) {
	p, m, err := e.plan(projectRoot, opt)
	if err != nil {
		return plan.ApplyResult{}, p, err
	}
	applier := plan.Applier{Now: e.now, LockWait: lockWait}
	result, err := applier.Apply(p.Changes, e.Backups(), &m, e.Env.ManifestPath(), e.Env.LockPath())
	return result, p, err
}

func (e Engine) Backups() backup.Store {
	s := e.Store
	if s.Env.Home == "" {
		s.Env = e.Env
	}
	if s.Now == nil {
		s.Now = e.Now
	}
	if s.Resolve == nil {
		s.Resolve = e.ResolveSecret
	}
	return s
}

func (e Engine) now() time.Time {
	if e.Now == nil {
		return time.Now()
	}
	return e.Now()
}

func (e Engine) plan(projectRoot string, opt plan.Options) (Plan, manifest.Manifest, error) {
	lib, err := e.scoped(projectRoot)
	if err != nil {
		return Plan{}, manifest.Manifest{}, err
	}
	m, err := manifest.Load(e.Env.ManifestPath())
	if err != nil {
		return Plan{}, manifest.Manifest{}, err
	}
	d, err := e.desired(projectRoot, lib, m)
	if err != nil {
		return Plan{}, m, err
	}
	opt.Keep = append(slices.Clone(opt.Keep), d.keep...)
	opt.Keep = append(opt.Keep, d.outOfScope(m)...)
	opt.Adopt = append(slices.Clone(opt.Adopt), e.Config.Adopted...)
	opt.ReplaceSymlinks = append(slices.Clone(opt.ReplaceSymlinks), e.Config.ReplaceSymlinks...)
	opt.SharedState = append(slices.Clone(opt.SharedState), d.sharedState...)
	changes, err := plan.Diff(d.units, m, d.tools(), opt)
	if err != nil {
		return Plan{}, m, err
	}
	return Plan{
		Changes:     changes,
		Unsupported: d.report(),
		Violations:  lib.violations,
		LibraryHash: lib.full.Hash(),
	}, m, nil
}

func (e Engine) adapterFor(tool string) (adapter.Adapter, error) {
	for _, a := range e.Adapters {
		if a.ID() == tool {
			return a, nil
		}
	}
	return nil, fmt.Errorf("unknown tool %q", tool)
}

func (e Engine) Restore(id, tool string, dryRun bool) (backup.RestorePlan, error) {
	restored, err := e.Backups().Restore(id, tool, dryRun)
	if err != nil || dryRun {
		return restored, err
	}
	var paths []string
	for _, f := range restored.Snapshot.Files {
		if tool == "" || slices.Contains(strings.Split(f.Tool, ","), tool) {
			paths = append(paths, f.Path)
		}
	}
	if err := e.reconcile(paths); err != nil {
		return restored, fmt.Errorf("reconcile manifest after restore: %w", err)
	}
	return restored, nil
}

func (e Engine) reconcile(paths []string) error {
	release, err := fsx.Acquire(e.Env.LockPath(), lockWait)
	if err != nil {
		return err
	}
	defer release()
	m, err := manifest.Load(e.Env.ManifestPath())
	if err != nil {
		return err
	}
	removed, err := plan.Reconcile(&m, paths)
	if err != nil || len(removed) == 0 {
		return err
	}
	return m.Save(e.Env.ManifestPath())
}
