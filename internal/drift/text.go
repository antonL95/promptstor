package drift

import (
	"cmp"
	"fmt"
	"io"
)

const noTool = "-"

func RenderText(w io.Writer, r Report) {
	findings := sorted(r.Findings)
	separator := ""
	if r.Clean() {
		_, _ = fmt.Fprintln(w, "Everything is in sync.")
		separator = "\n"
	}
	counts := r.counts()
	var current FindingKind
	for _, f := range findings {
		if f.Kind != current {
			current = f.Kind
			_, _ = fmt.Fprintf(w, "%s%s (%d)\n", separator, f.Kind, counts[f.Kind])
			separator = "\n"
		}
		_, _ = fmt.Fprintf(w, "  %s  %s  %s\n", cmp.Or(f.Tool, noTool), cmp.Or(f.Name, f.Path), f.Detail)
	}
}
