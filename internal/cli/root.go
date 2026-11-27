package cli

import (
	"io"

	"github.com/spf13/cobra"
)

type Deps struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func NewRoot(d Deps) *cobra.Command {
	root := &cobra.Command{
		Use:          "promptstor",
		Short:        "Keep coding agent configuration in one library",
		SilenceUsage: true,
	}
	root.SetIn(d.Stdin)
	root.SetOut(d.Stdout)
	root.SetErr(d.Stderr)
	root.AddCommand(newVersionCommand())
	return root
}
