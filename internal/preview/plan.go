package preview

import (
	"cmp"
	"fmt"
	"io"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/plan"
)

type writer struct {
	w   io.Writer
	err error
}

func (p *writer) printf(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, args...)
	}
}

func Plan(out io.Writer, p engine.Plan, diffs bool) (int, error) {
	w := &writer{w: out}
	changing, blocked := 0, 0
	for _, c := range p.Changes {
		if c.Blocked == "" && c.Op != plan.OpNoop {
			changing++
			if diffs {
				w.printf("%s", Change(c))
			}
		}
	}
	for _, c := range p.Changes {
		if c.Blocked != "" {
			if blocked == 0 {
				w.printf("blocked:\n")
			}
			blocked++
			w.printf("  %s: %s\n", c.Path, c.Blocked)
		}
	}
	if len(p.Violations) > 0 {
		w.printf("locked:\n")
		for _, v := range p.Violations {
			w.printf("  %s (%s): the %s layer cannot change it, %s\n", v.Name, v.ItemID, v.Layer, v.Reason)
		}
	}
	withheld := unsupported(w, p.Unsupported)
	w.printf("%d files to change, %d blocked, %d unsupported\n", changing, blocked, len(p.Unsupported))
	if w.err != nil {
		return 0, fmt.Errorf("print: %w", w.err)
	}
	return blocked + len(p.Violations) + withheld, nil
}

func unsupported(w *writer, report []adapter.Unsupported) int {
	if len(report) == 0 {
		return 0
	}
	sorted := slices.Clone(report)
	slices.SortStableFunc(sorted, func(a, b adapter.Unsupported) int {
		return cmp.Or(cmp.Compare(a.Tool, b.Tool), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Reason, b.Reason))
	})
	w.printf("unsupported:\n")
	withheld := 0
	for i, u := range sorted {
		if i == 0 || u.Tool != sorted[i-1].Tool {
			w.printf("  %s\n", u.Tool)
		}
		suffix := ""
		if u.Blocking {
			withheld++
			suffix = " (withheld until acknowledged)"
		}
		w.printf("    %s: %s%s\n", cmp.Or(u.Name, string(u.ItemID)), u.Reason, suffix)
	}
	return withheld
}
