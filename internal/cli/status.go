package cli

import (
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/paths"
)

func newStatusCommand(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the last check, the library hash and the selected tools",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := d.engine()
			if err != nil {
				return err
			}
			return printStatus(cmd.OutOrStdout(), e)
		},
	}
}

func printStatus(out io.Writer, e engine.Engine) error {
	w := &printer{w: out}
	if err := printLastCheck(w, e.Env); err != nil {
		return err
	}
	merged, _, err := e.Library("")
	if err != nil {
		return err
	}
	w.printf("library: %s\n", merged.Hash())
	tools := "none selected"
	if len(e.Config.Tools) > 0 {
		tools = strings.Join(e.Config.Tools, ", ")
	}
	w.printf("tools: %s\n", tools)
	return w.done()
}

func printLastCheck(w *printer, env paths.Env) error {
	check, found, err := drift.ReadLastCheck(env)
	if err != nil {
		return err
	}
	if !found {
		w.printf("no check has run yet\n")
		return nil
	}
	w.printf("last check: %s\n", check.Checked.UTC().Format(time.RFC3339))
	for _, kind := range slices.Sorted(maps.Keys(check.Counts)) {
		w.printf("  %s: %d\n", kind, check.Counts[kind])
	}
	return nil
}
