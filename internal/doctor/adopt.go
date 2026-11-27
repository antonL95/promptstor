package doctor

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/mdrules"
)

const (
	questionMove   = "Move the rules of %s into PromptStor's managed section?"
	questionDelete = "Delete the original %s?"
	rulesDirName   = "rules"
)

type moveRule struct {
	it    library.Item
	owner *candidate
}

type move struct {
	path, root string
	rules      []moveRule
	accepted   bool
	before     []byte
	after      []byte
	mode       fs.FileMode
}

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

func (d *doctor) collectMoves(scopes []scopeImports, cands []*candidate, selected []string) []*move {
	byPath := map[string]*move{}
	for _, s := range scopes {
		local := slices.DeleteFunc(slices.Clone(cands), func(c *candidate) bool { return c.target.Scope != s.target.Scope })
		for _, tool := range selected {
			for _, it := range s.byTool[tool].Items {
				if !handWritten(it) || titleOnlyRule(it) {
					continue
				}
				owner := ownerOf(it, local)
				if owner == nil {
					continue
				}
				m := byPath[it.Source]
				if m == nil {
					m = &move{path: it.Source, root: scopeRoot(s.target)}
					byPath[it.Source] = m
				}
				if !slices.ContainsFunc(m.rules, func(r moveRule) bool { return r.it.Name == it.Name && r.it.Order == it.Order }) {
					m.rules = append(m.rules, moveRule{it: it, owner: owner})
				}
			}
		}
	}
	moves := make([]*move, 0, len(byPath))
	for _, m := range byPath {
		moves = append(moves, m)
	}
	slices.SortFunc(moves, func(a, b *move) int { return cmp.Compare(a.path, b.path) })
	return moves
}

func ownerOf(it library.Item, cands []*candidate) *candidate {
	for _, c := range cands {
		if c.it.Source == it.Source && c.it.Name == it.Name && c.it.Order == it.Order && c.it.Kind == it.Kind {
			return c
		}
	}
	return coveredBy(it, cands)
}

func (d *doctor) askMoves(moves []*move) error {
	for _, m := range moves {
		link, linked, err := fsx.LinkedAncestor(m.root, m.path)
		switch {
		case err != nil:
			d.w.printf("%s cannot be changed (%v); its rules stay where they are\n", m.path, err)
			continue
		case linked:
			d.w.printf("%s goes through the symlink %s; its rules stay where they are\n", m.path, link)
			continue
		}
		var names []string
		for _, r := range m.rules {
			names = append(names, r.it.Name)
		}
		d.w.printf("\n%s holds rules the library will manage: %s\n", m.path, strings.Join(names, ", "))
		ok, err := d.p.Confirm(fmt.Sprintf(questionMove, m.path), true)
		if err != nil {
			return err
		}
		m.accepted = ok
		if !ok {
			dropFrom(m.path, m.rules)
		}
	}
	return nil
}

func dropFrom(path string, rules []moveRule) {
	for _, r := range rules {
		if r.owner.it.Source == path {
			r.owner.drop = true
		}
	}
}

func (d *doctor) prepareMoves(moves []*move, requireStored bool) error {
	for _, m := range moves {
		m.before, m.after = nil, nil
		if !m.accepted {
			continue
		}
		var rules []library.Item
		for _, r := range m.rules {
			if r.owner.kept() && (r.owner.stored || !requireStored) {
				rules = append(rules, r.it)
			}
		}
		if len(rules) == 0 {
			continue
		}
		info, err := os.Stat(m.path)
		if err != nil {
			return fmt.Errorf("read %s: %w", m.path, err)
		}
		before, err := os.ReadFile(m.path)
		if err != nil {
			return fmt.Errorf("read %s: %w", m.path, err)
		}
		after, err := mdrules.Remove(before, rules)
		if err != nil {
			d.w.printf("cannot find the rules in %s (%v); its rules are not pulled\n", m.path, err)
			m.accepted = false
			dropFrom(m.path, m.rules)
			continue
		}
		m.before, m.after, m.mode = before, after, info.Mode().Perm()
	}
	return nil
}

func (d *doctor) collectFiles(cands []*candidate) (adoption, error) {
	var a adoption
	bySource := map[string]*deletion{}
	for _, c := range keptCandidates(cands) {
		switch c.it.Kind {
		case library.KindPermission, library.KindHook, library.KindMCP:
			continue
		}
		if handWritten(c.it) || c.it.Source == "" {
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
			default:
				elsewhere = append(elsewhere, u.Path)
			}
		}
		if len(same) > 0 {
			for _, path := range same {
				if entry, err := fsx.Inspect(path); err == nil && entry.Exists && !slices.Contains(a.adopt, path) {
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
