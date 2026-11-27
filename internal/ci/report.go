package ci

import (
	"cmp"
	"fmt"
	"io"
	"path/filepath"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/preview"
)

type report struct {
	w        io.Writer
	root     string
	problems int
	err      error
}

func (r *report) printf(format string, args ...any) {
	if r.err == nil {
		_, r.err = fmt.Fprintf(r.w, format, args...)
	}
}

func (r *report) problem(format string, args ...any) {
	r.problems++
	r.printf(format, args...)
}

func (r *report) relative(path string) string {
	relative, err := filepath.Rel(r.root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(relative)
}

func (r *report) compare(c plan.FileChange) error {
	name := r.relative(c.Path)
	if c.Blocked != "" {
		r.problem("blocked: %s: %s\n", name, c.Blocked)
		return nil
	}
	for _, u := range c.Units {
		current, found, err := plan.ExtractUnit(u.Kind, c.Before, u.Key, c.Exists)
		if err != nil {
			return fmt.Errorf("check %s: %w", c.Path, err)
		}
		state := "missing"
		if found {
			same, err := sameHash(u.Kind, u.Content, current)
			if err != nil {
				return fmt.Errorf("check %s: %w", c.Path, err)
			}
			if same {
				continue
			}
			state = "differs"
		}
		shown := u
		shown.Path = name
		r.problem("%s: %s (%s)\n", state, name, cmp.Or(u.Key, string(u.Kind)))
		r.printf("%s", preview.Change(plan.FileChange{Path: name, Units: []plan.Unit{shown}, Before: current, After: u.Content}))
	}
	return nil
}

func sameHash(kind manifest.UnitKind, want, have []byte) (bool, error) {
	wanted, err := manifest.Hash(kind, want)
	if err != nil {
		return false, err
	}
	found, err := manifest.Hash(kind, have)
	if err != nil {
		return false, err
	}
	return wanted == found, nil
}

func (r *report) literals(found []literal) {
	for _, l := range found {
		r.problem("literal secret in %s (%s); use an env placeholder\n", l.item, l.key)
	}
}

func (r *report) unsupported(all []adapter.Unsupported) {
	sorted := slices.Clone(all)
	slices.SortFunc(sorted, func(a, b adapter.Unsupported) int {
		return cmp.Or(
			cmp.Compare(a.Tool, b.Tool),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.ItemID, b.ItemID),
			cmp.Compare(a.Reason, b.Reason),
		)
	})
	for _, u := range slices.Compact(sorted) {
		line := fmt.Sprintf("%s: %s: %s\n", u.Tool, cmp.Or(u.Name, string(u.ItemID)), u.Reason)
		if u.Blocking {
			r.problem("blocked: %s", line)
			continue
		}
		r.printf("note: %s", line)
	}
}

func (r *report) finish() (int, error) {
	switch r.problems {
	case 0:
		r.printf("in sync\n")
	case 1:
		r.printf("1 problem\n")
	default:
		r.printf("%d problems\n", r.problems)
	}
	switch {
	case r.err != nil:
		return codeError, fmt.Errorf("print: %w", r.err)
	case r.problems > 0:
		return codeDiffers, nil
	default:
		return codeInSync, nil
	}
}
