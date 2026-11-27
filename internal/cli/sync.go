package cli

import (
	"cmp"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/preview"
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
	if w.err != nil {
		return 0
	}
	blocked, err := preview.Plan(w.w, p, true)
	w.err = err
	return blocked
}
