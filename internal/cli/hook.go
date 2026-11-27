package cli

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/guard"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
)

type ExitCode int

func (c ExitCode) Error() string { return fmt.Sprintf("exit status %d", int(c)) }

func newHookCommand(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:           "hook <tool>",
		Short:         "Check an agent's edit against the files PromptStor owns",
		Hidden:        true,
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if code := guard.Run(args[0], d.Env.ManifestPath(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()); code != 0 {
				return ExitCode(code)
			}
			return nil
		},
	}
}

const reasonNoAntigravityGuard = "Antigravity hooks must decide every call and have no neutral answer; no edit guard in this release"

func editGuardNotices(cfg config.Config) []adapter.Unsupported {
	if !cfg.EditGuards || !slices.Contains(cfg.Tools, adapter.Antigravity) {
		return nil
	}
	return []adapter.Unsupported{{Tool: adapter.Antigravity, Name: "edit guard " + adapter.Antigravity, Reason: reasonNoAntigravityGuard}}
}

func (d Deps) editGuards(cfg config.Config) ([]library.Item, error) {
	if !cfg.EditGuards || d.Binary == "" {
		return nil, nil
	}
	var guards []library.Item
	for _, tool := range library.GuardedTools() {
		if slices.Contains(cfg.Tools, tool) {
			guards = append(guards, library.EditGuard(tool, d.Binary))
		}
	}
	if !slices.Contains(cfg.Tools, adapter.OpenCode) {
		return guards, nil
	}
	m, err := manifest.Load(d.Env.ManifestPath())
	if err != nil {
		return nil, err
	}
	if owned := guard.WholeFiles(m, cfg.Tools); len(owned) > 0 {
		guards = append(guards, library.OpencodeGuard(owned))
	}
	return guards, nil
}
