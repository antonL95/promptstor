package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/antonL95/promptstor/internal/engine"
)

type lastCheck struct {
	Checked  time.Time `json:"checked"`
	Findings []struct {
		Kind string `json:"kind"`
	} `json:"findings"`
}

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
	if err := printLastCheck(w, e.Env.LastCheckPath()); err != nil {
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

func printLastCheck(w *printer, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		w.printf("no check has run yet\n")
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var check lastCheck
	if err := json.Unmarshal(data, &check); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	w.printf("last check: %s\n", check.Checked.UTC().Format(time.RFC3339))
	counts := map[string]int{}
	for _, finding := range check.Findings {
		counts[finding.Kind]++
	}
	for _, kind := range slices.Sorted(maps.Keys(counts)) {
		w.printf("  %s: %d\n", kind, counts[kind])
	}
	return nil
}
