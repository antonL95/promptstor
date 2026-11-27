package ci

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/adapter/agentsmd"
	_ "github.com/antonL95/promptstor/internal/adapter/all"
	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/policy"
)

const (
	layerDir   = ".promptstor"
	configName = "config.yaml"
)

type project struct {
	root  string
	tools []adapter.ToolID
	items library.Merged
	acks  policy.Acks
}

type export struct {
	units       []plan.Unit
	unsupported []adapter.Unsupported
	literals    []literal
}

func load(root string) (project, error) {
	tools, err := loadTools(root)
	if err != nil {
		return project{}, err
	}
	dir := filepath.Join(root, layerDir)
	items, err := library.LoadLayer(dir, library.LayerProject, library.LoadOptions{})
	if err != nil {
		return project{}, fmt.Errorf("load %s: %w", dir, err)
	}
	merged, _ := library.Merge([]library.LayerItems{{Layer: library.LayerProject, Items: items}})
	merged.Items = slices.DeleteFunc(merged.Items, func(it library.Item) bool { return library.IsBuiltin(it.ID) })
	committed, err := policy.LoadProjectAcks(root)
	if err != nil {
		return project{}, err
	}
	acks := policy.Acks{}.WithProject(committed, merged.Items)
	return project{root: root, tools: tools, items: merged, acks: acks}, nil
}

func loadTools(root string) ([]adapter.ToolID, error) {
	file := filepath.Join(root, layerDir, configName)
	link, linked, err := fsx.LinkedAncestor(root, file)
	if err != nil {
		return nil, err
	}
	if linked {
		return nil, fmt.Errorf("%s is symlinked, which is never followed", link)
	}
	cfg, found, err := config.LoadProject(root)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("no %s/%s in %s", layerDir, configName, root)
	}
	if len(cfg.Tools) == 0 {
		return nil, fmt.Errorf("%s lists no tools", file)
	}
	for _, tool := range cfg.Tools {
		if _, known := adapter.Get(tool); !known {
			return nil, fmt.Errorf("%s: unknown tool %q", file, tool)
		}
	}
	tools := slices.Clone(cfg.Tools)
	if agentsmd.NeededFor(tools) {
		tools = append(tools, adapter.AgentsMD)
	}
	slices.Sort(tools)
	return slices.Compact(tools), nil
}

func (p project) export() (export, error) {
	scratch, err := os.MkdirTemp("", "promptstor-check-")
	if err != nil {
		return export{}, fmt.Errorf("make a scratch folder: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	target := adapter.Target{
		Env:         emptyEnv(filepath.Join(scratch, "home")),
		Scope:       adapter.ScopeProject,
		ProjectRoot: p.root,
		Selected:    p.tools,
	}
	var out export
	for _, tool := range p.tools {
		a, known := adapter.Get(tool)
		if !known {
			return export{}, fmt.Errorf("unknown tool %q", tool)
		}
		items := p.items.Items
		if tool != adapter.AgentsMD {
			items = p.items.ForTool(tool)
		}
		units, unsupported, err := a.Export(target, items)
		if err != nil {
			return export{}, fmt.Errorf("export %s: %w", tool, err)
		}
		out.units = append(out.units, units...)
		for _, u := range unsupported {
			out.unsupported = append(out.unsupported, p.acknowledge(u, tool))
		}
	}
	out.literals = literalSecrets(p.items.Items)
	hide(out.units, out.literals)
	return out, nil
}

func (p project) acknowledge(u adapter.Unsupported, tool adapter.ToolID) adapter.Unsupported {
	at := slices.IndexFunc(p.items.Items, func(it library.Item) bool { return it.ID == u.ItemID })
	if at < 0 {
		return u
	}
	return p.acks.Acknowledge(u, p.items.Items[at], cmp.Or(u.Tool, tool))
}

func emptyEnv(home string) paths.Env {
	return paths.Env{
		Home:   home,
		OS:     "linux",
		Getenv: func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
		},
	}.WithBaseDirs()
}
