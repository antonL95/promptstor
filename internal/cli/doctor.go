package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/antonL95/promptstor/internal/doctor"
	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/paths"
)

type noNotifier struct{}

func (noNotifier) Notify(paths.Env, drift.Report) error { return nil }

func newDoctorCommand(d Deps) *cobra.Command {
	var opt doctor.Options
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Set up PromptStor on the first run, then report and resolve drift",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := d.engine()
			if err != nil {
				return err
			}
			if opt.Project, err = absolute(opt.Project); err != nil {
				return err
			}
			opt.Notifier = noNotifier{}
			opt.Reconfigure = d.engineFor
			p := doctor.LinePrompter(cmd.InOrStdin(), cmd.OutOrStdout(), d.TTY, opt.Yes)
			code, err := doctor.Run(e, p, cmd.OutOrStdout(), opt)
			if err != nil && opt.Check {
				return errors.Join(err, ExitCode(code))
			}
			if err != nil {
				return err
			}
			if code != 0 {
				return ExitCode(code)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&opt.DryRun, "dry-run", false, "ask and preview, but write nothing at all")
	cmd.Flags().BoolVar(&opt.Check, "check", false, "report drift without asking; exit 0 when clean, 1 on drift, 2 on error")
	cmd.Flags().BoolVar(&opt.Notify, "notify", false, "send a notification with the check result")
	cmd.Flags().BoolVar(&opt.Yes, "yes", false, "take the default answer to every question")
	cmd.Flags().BoolVar(&opt.JSON, "json", false, "print the drift report as JSON")
	cmd.Flags().StringVar(&opt.Primary, "primary", "", "use this tool as the primary on the first run")
	cmd.Flags().StringSliceVar(&opt.Tools, "tools", nil, "keep these tools in sync on the first run")
	cmd.Flags().StringVar(&opt.Project, "project", "", "also include the project in this directory")
	return cmd
}
