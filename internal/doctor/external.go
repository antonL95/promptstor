package doctor

import (
	"path/filepath"
	"slices"

	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/plan"
)

const collapseAbove = 5

const questionExternal = "PromptStor would change these files. Which of them does another tool or script write? PromptStor will leave those alone."

func (d *doctor) askExternal(s *firstRunState) error {
	e, err := d.engineFor(config.Config{Version: 1, Primary: s.primary, Tools: s.selected})
	if err != nil {
		return err
	}
	e.Builtins = append(slices.Clone(e.Builtins), candidateItems(s.cands)...)
	p, err := e.Plan(d.opt.Project, plan.Options{})
	if err != nil {
		return err
	}
	var files, blocked []string
	for _, c := range p.Changes {
		switch {
		case c.Blocked == plan.ReasonUnmanaged:
		case c.Blocked != "":
			blocked = append(blocked, c.Path)
		case c.Op != plan.OpNoop:
			files = append(files, c.Path)
		}
	}
	for _, c := range keptCandidates(s.cands) {
		if c.it.Source != "" && writable(scopeRoot(c.target), c.it.Source, blocked) {
			files = append(files, c.it.Source)
		}
	}
	slices.Sort(files)
	files = collapse(slices.Compact(files))
	if len(files) == 0 {
		return nil
	}
	picked, err := d.p.MultiSelect(questionExternal, files, nil)
	if err != nil {
		return err
	}
	for _, at := range picked {
		s.external = append(s.external, files[at])
	}
	slices.Sort(s.external)
	return nil
}

func writable(root, source string, blocked []string) bool {
	if _, linked, err := fsx.LinkedAncestor(root, source); err != nil || linked {
		return false
	}
	return !slices.ContainsFunc(blocked, func(path string) bool {
		_, below := fsx.Within([]string{source}, path)
		return below
	})
}

func collapse(files []string) []string {
	perDir := map[string]int{}
	for _, f := range files {
		perDir[filepath.Dir(f)]++
	}
	var out []string
	for _, f := range files {
		if dir := filepath.Dir(f); perDir[dir] > collapseAbove {
			out = append(out, dir)
			continue
		}
		out = append(out, f)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func candidateItems(cands []*candidate) []library.Item {
	var items []library.Item
	for _, c := range keptCandidates(cands) {
		it := c.it
		it.Layer = scopeLayer(c.target.Scope)
		items = append(items, it)
	}
	return items
}

func (s *firstRunState) isExternal(path string) bool {
	return plan.External(s.external, path)
}
