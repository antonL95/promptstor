package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/antonL95/promptstor/internal/schedule"
)

const scheduleRemoved = "Scheduled checks removed."

func newScheduleCommand(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage the hourly drift check",
	}
	scheduler := schedule.Scheduler{Env: d.Env, Binary: d.Binary, Run: d.run, Getuid: os.Getuid}
	cmd.AddCommand(
		scheduleAction("install", "Check for drift every hour and notify on changes", scheduler.Install),
		scheduleAction("remove", "Stop the hourly check", func() (string, error) {
			if d.Env.OS == "windows" {
				return schedule.WindowsDeferred, nil
			}
			return scheduleRemoved, scheduler.Remove()
		}),
		scheduleAction("status", "Show whether the hourly check is installed", scheduler.Status),
	)
	return cmd
}

func scheduleAction(use, short string, action func() (string, error)) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			message, err := action()
			if err != nil {
				return err
			}
			w := &printer{w: cmd.OutOrStdout()}
			w.printf("%s\n", message)
			return w.done()
		},
	}
}
