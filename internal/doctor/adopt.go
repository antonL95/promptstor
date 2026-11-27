package doctor

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
)

const (
	questionDelete = "Delete the original %s?"
	rulesDirName   = "rules"
)

type deletion struct {
	path, root string
	owners     []*candidate
}

type adoption struct {
	deletions []*deletion
	adopt     []string
}

func handWritten(it library.Item) bool {
	return it.Kind == library.KindRule && it.Source != "" && filepath.Base(filepath.Dir(it.Source)) != rulesDirName
}

func scopeRoot(t adapter.Target) string {
	if t.Scope == adapter.ScopeProject {
		return t.ProjectRoot
	}
	return t.Env.Home
}

func (d *doctor) collectFiles(cands []*candidate, external func(string) bool) (adoption, error) {
	var a adoption
	bySource := map[string]*deletion{}
	for _, c := range keptCandidates(cands) {
		switch c.it.Kind {
		case library.KindPermission, library.KindHook, library.KindMCP:
			continue
		}
		if handWritten(c.it) || c.it.Source == "" || external(c.it.Source) {
			continue
		}
		ad, ok := d.adapter(c.tool)
		if !ok {
			return adoption{}, fmt.Errorf("unknown tool %q", c.tool)
		}
		t := c.target
		t.Selected = []string{c.tool}
		units, _, err := ad.Export(t, []library.Item{c.it})
		if err != nil {
			return adoption{}, fmt.Errorf("export %s %q: %w", c.it.Kind, c.it.Name, err)
		}
		var elsewhere, same []string
		for _, u := range units {
			switch {
			case u.Kind == manifest.UnitFile && (u.Path == c.it.Source || strings.HasPrefix(u.Path, c.it.Source+string(filepath.Separator))):
				same = append(same, u.Path)
			case !external(u.Path):
				elsewhere = append(elsewhere, u.Path)
			}
		}
		if len(same) > 0 {
			for _, path := range same {
				if entry, err := fsx.Inspect(path); err == nil && entry.Exists && !external(path) && !slices.Contains(a.adopt, path) {
					a.adopt = append(a.adopt, path)
				}
			}
			continue
		}
		if len(elsewhere) == 0 {
			continue
		}
		del := bySource[c.it.Source]
		if del == nil {
			del = &deletion{path: c.it.Source, root: scopeRoot(c.target)}
			bySource[c.it.Source] = del
			a.deletions = append(a.deletions, del)
			d.w.printf("\n%s would exist twice after sync (PromptStor writes it as %s)\n", c.it.Source, strings.Join(elsewhere, ", "))
		}
		del.owners = append(del.owners, c)
	}
	slices.Sort(a.adopt)
	return a, d.askDeletions(a.deletions)
}

func (d *doctor) askDeletions(deletions []*deletion) error {
	for _, del := range deletions {
		ok, err := d.p.Confirm(fmt.Sprintf(questionDelete, del.path), false)
		if err != nil {
			return err
		}
		if !ok {
			for _, c := range del.owners {
				c.drop = true
			}
		}
	}
	return nil
}

func (del *deletion) wanted() bool {
	return slices.ContainsFunc(del.owners, func(c *candidate) bool { return c.kept() && c.stored })
}
