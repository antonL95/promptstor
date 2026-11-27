package doctor

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/adapter/agentsmd"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/mdrules"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	questionMove = "Move the rules of %s into PromptStor's managed section?"
	noteBlocked  = "%s is blocked (%s); PromptStor edits nothing there and still pulls its rules for the other tools\n"
)

type moveRule struct {
	it    library.Item
	owner *candidate
}

type move struct {
	path, root string
	reader     string
	rules      []moveRule
	imports    []importLine
	accepted   bool
	before     []byte
	after      []byte
	mode       fs.FileMode
}

type exportTargets struct {
	owner map[string]string
	units map[string][]plan.Unit
}

func (d *doctor) collectMoves(scopes []scopeImports, cands []*candidate, selected []string) ([]*move, error) {
	m, err := manifest.Load(d.e.Env.ManifestPath())
	if err != nil {
		return nil, err
	}
	var moves []*move
	for _, s := range scopes {
		local := slices.DeleteFunc(slices.Clone(cands), func(c *candidate) bool { return c.target.Scope != s.target.Scope || !c.kept() })
		tools := slices.Clone(selected)
		if s.target.Scope == adapter.ScopeProject && agentsmd.NeededFor(tools) {
			tools = append(tools, adapter.AgentsMD)
		}
		targets, err := d.exportTargets(s.target, tools, local)
		if err != nil {
			return nil, err
		}
		for _, path := range slices.Sorted(maps.Keys(targets.owner)) {
			if reason, err := blockedReason(targets.units[path], m, tools); err != nil || reason != "" {
				d.w.printf(noteBlocked, path, firstOf(reason, err))
				continue
			}
			mv, err := d.targetMove(s, path, local, targets)
			if err != nil {
				return nil, err
			}
			if mv != nil {
				moves = append(moves, mv)
			}
		}
	}
	return moves, nil
}

func firstOf(reason string, err error) string {
	if err != nil {
		return err.Error()
	}
	return reason
}

func (d *doctor) exportTargets(t adapter.Target, tools []string, local []*candidate) (exportTargets, error) {
	targets := exportTargets{owner: map[string]string{}, units: map[string][]plan.Unit{}}
	var rules []library.Item
	for _, c := range local {
		if c.it.Kind == library.KindRule {
			rules = append(rules, c.it)
		}
	}
	if len(rules) == 0 {
		return targets, nil
	}
	t.Selected = tools
	for _, tool := range tools {
		a, ok := d.adapter(tool)
		if !ok {
			return targets, fmt.Errorf("unknown tool %q", tool)
		}
		units, _, err := a.Export(t, rules)
		if err != nil {
			return targets, fmt.Errorf("export %s for %s scope: %w", tool, t.Scope, err)
		}
		for _, u := range units {
			if u.Kind != manifest.UnitSection {
				continue
			}
			if targets.owner[u.Path] == "" {
				targets.owner[u.Path] = tool
			}
			targets.units[u.Path] = append(targets.units[u.Path], u)
		}
	}
	return targets, nil
}

func blockedReason(units []plan.Unit, m manifest.Manifest, tools []string) (string, error) {
	changes, err := plan.Diff(units, m, tools, plan.Options{})
	if err != nil {
		return "", err
	}
	for _, c := range changes {
		if c.Blocked != "" {
			return c.Blocked, nil
		}
	}
	return "", nil
}

func (d *doctor) targetMove(s scopeImports, path string, local []*candidate, targets exportTargets) (*move, error) {
	mv := &move{path: path, root: scopeRoot(s.target)}
	for _, tool := range s.ordered {
		for _, it := range s.byTool[tool].Items {
			if it.Source != path || !handWritten(it) {
				continue
			}
			if mv.reader == "" {
				mv.reader = tool
			}
			if tool != mv.reader || titleOnlyRule(it) {
				continue
			}
			if owner := ownerOf(it, local); owner != nil {
				mv.rules = append(mv.rules, moveRule{it: it, owner: owner})
			}
		}
	}
	if mv.reader == "" {
		return nil, nil
	}
	imports, err := d.pulledImports(s, mv, local, targets)
	if err != nil {
		return nil, err
	}
	mv.imports = imports
	if len(mv.rules) == 0 && len(mv.imports) == 0 {
		return nil, nil
	}
	return mv, nil
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
		var names []string
		for _, r := range m.rules {
			names = append(names, r.it.Name)
		}
		for _, imp := range m.imports {
			names = append(names, "the import of "+imp.target+" (all of its rules are pulled)")
		}
		d.w.printf("\n%s holds what the library will manage: %s\n", m.path, strings.Join(names, ", "))
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
	ready := func(c *candidate) bool { return c.kept() && (c.stored || !requireStored) }
	for _, m := range moves {
		m.before, m.after = nil, nil
		if !m.accepted {
			continue
		}
		var rules []library.Item
		for _, r := range m.rules {
			if ready(r.owner) {
				rules = append(rules, r.it)
			}
		}
		var lines []int
		for _, imp := range m.imports {
			if !slices.ContainsFunc(imp.owners, func(c *candidate) bool { return !ready(c) }) {
				lines = append(lines, imp.line)
			}
		}
		if len(rules) == 0 && len(lines) == 0 {
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
		after, err := mdrules.Remove(removeLines(before, lines), m.path, rules, removeOptions(m.reader))
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
