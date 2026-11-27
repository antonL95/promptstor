package doctor

import (
	"slices"

	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/plan"
)

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
	var files []string
	for _, c := range p.Changes {
		if c.Blocked == "" && (c.Op != plan.OpNoop || len(c.Removed) > 0) {
			files = append(files, c.Path)
		}
	}
	for _, c := range keptCandidates(s.cands) {
		if c.it.Source != "" {
			files = append(files, c.it.Source)
		}
	}
	slices.Sort(files)
	files = slices.Compact(files)
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
