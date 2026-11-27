package cli

import (
	"io"
	"time"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/spf13/cobra"
)

type Deps struct {
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Env      paths.Env
	Now      func() time.Time
	Run      func(name string, args ...string) ([]byte, error)
	RunIn    func(dir, name string, args ...string) ([]byte, error)
	Builtins []library.Item
	Binary   string
	TTY      bool
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
	root.AddCommand(newVersionCommand(), newSyncCommand(d), newRestoreCommand(d), newStatusCommand(d), newHookCommand(d), newMCPCommand(d), newDoctorCommand(d), newCheckCommand(), newScheduleCommand(d), newAdaptersCommand())
	return root
}
