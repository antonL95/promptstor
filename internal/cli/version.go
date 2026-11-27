package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var version = "dev"

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the promptstor version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "promptstor %s\n", version); err != nil {
				return fmt.Errorf("print version: %w", err)
			}
			return nil
		},
	}
}
