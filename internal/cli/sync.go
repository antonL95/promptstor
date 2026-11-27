package cli

import (
	"cmp"
	"fmt"
	"io"
	"slices"

	"github.com/spf13/cobra"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/plan"
)

type syncFlags struct {
	dryRun         bool
	project        string
	replaceSymlink []string
}

func newSyncCommand(d Deps) *cobra.Command {
	var f syncFlags
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Write the library into every selected tool",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd.OutOrStdout(), d, f)
		},
	}
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "print the changes without writing them")
	cmd.Flags().StringVar(&f.project, "project", "", "also sync the project in this directory")
	cmd.Flags().StringArrayVar(&f.replaceSymlink, "replace-symlink", nil, "replace this symlink with a file")
	return cmd
}

func runSync(w io.Writer, d Deps, f syncFlags) error {
	e, err := d.engine()
	if err != nil {
		return err
	}
	root, err := absolute(f.project)
	if err != nil {
		return err
	}
	var opt plan.Options
	for _, path := range f.replaceSymlink {
		abs, err := absolute(path)
		if err != nil {
			return err
		}
		opt.ReplaceSymlinks = append(opt.ReplaceSymlinks, abs)
	}
	out := &printer{w: w}
	if f.dryRun {
		p, err := e.Plan(root, opt)
		if err != nil {
			return err
		}
		blocked := printPlan(out, p)
		return cmp.Or(out.done(), outcome(blocked))
	}
	result, p, err := e.Sync(root, opt)
	if err != nil {
		return err
	}
	blocked := printPlan(out, p)
	if result.SnapshotID != "" {
		out.printf("snapshot %s\n", result.SnapshotID)
	}
	for _, path := range result.Written {
		out.printf("wrote %s\n", path)
	}
	return cmp.Or(out.done(), outcome(blocked))
}

func outcome(blocked int) error {
	if blocked > 0 {
		return fmt.Errorf("%d blocked", blocked)
	}
	return nil
}

func printPlan(w *printer, p engine.Plan) int {
	changing, blocked := 0, 0
	for _, c := range p.Changes {
		if c.Blocked == "" && c.Op != plan.OpNoop {
			changing++
			w.printf("%s", describeChange(c))
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
	withheld := printUnsupported(w, p.Unsupported)
	w.printf("%d files to change, %d blocked, %d unsupported\n", changing, blocked, len(p.Unsupported))
	return blocked + len(p.Violations) + withheld
}

func printUnsupported(w *printer, report []adapter.Unsupported) int {
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
