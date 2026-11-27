package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/antonL95/promptstor/internal/ci"
)

const checkErrorCode = ExitCode(2)

var errOnlyCI = errors.New("only check --ci is available")

func newCheckCommand() *cobra.Command {
	var (
		inCI bool
		dir  string
	)
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Compare the committed project files with the .promptstor folder",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !inCI {
				return errors.Join(errOnlyCI, checkErrorCode)
			}
			code, err := ci.Check(dir, cmd.OutOrStdout())
			if err != nil {
				return errors.Join(err, ExitCode(code))
			}
			if code != 0 {
				cmd.SilenceErrors = true
				return ExitCode(code)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&inCI, "ci", false, "use only the repository; exit 0 when in sync, 1 on a difference, 2 on an error")
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository to check")
	return cmd
}
