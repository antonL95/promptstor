package cli

import (
	"cmp"
	"errors"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/engine"
)

type restoreFlags struct {
	list   bool
	tool   string
	dryRun bool
}

func newRestoreCommand(d Deps) *cobra.Command {
	var f restoreFlags
	cmd := &cobra.Command{
		Use:   "restore [SNAPSHOT]",
		Short: "Put files back the way a snapshot recorded them",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := d.engine()
			if err != nil {
				return err
			}
			if f.list {
				return listSnapshots(cmd.OutOrStdout(), e.Backups())
			}
			if len(args) != 1 {
				return errors.New("name a snapshot, or list them with --list")
			}
			return runRestore(cmd.OutOrStdout(), e, args[0], f)
		},
	}
	cmd.Flags().BoolVar(&f.list, "list", false, "list snapshots, newest first")
	cmd.Flags().StringVar(&f.tool, "tool", "", "restore only the files of this tool")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "print what would be restored without changing anything")
	return cmd
}

func listSnapshots(out io.Writer, store backup.Store) error {
	snaps, err := store.List()
	if err != nil {
		return err
	}
	w := &printer{w: out}
	if len(snaps) == 0 {
		w.printf("no snapshots\n")
		return w.done()
	}
	for _, snap := range snaps {
		w.printf("%s  %s  %s  %d files\n", snap.ID, snap.Created.UTC().Format(time.RFC3339), snap.Reason, len(snap.Files))
	}
	return w.done()
}

func runRestore(out io.Writer, e engine.Engine, id string, f restoreFlags) error {
	restored, err := e.Restore(id, f.tool, f.dryRun)
	w := &printer{w: out}
	if restored.PreRestoreID != "" {
		w.printf("pre-restore snapshot %s (undo with: promptstor restore %s)\n", restored.PreRestoreID, restored.PreRestoreID)
	}
	if err != nil {
		return errors.Join(err, w.done())
	}
	blocked := 0
	for _, a := range restored.Actions {
		if a.Op == backup.OpBlocked {
			blocked++
		}
		line := a.Op + " " + a.Path
		if a.Detail != "" {
			line += ": " + a.Detail
		}
		w.printf("%s\n", line)
	}
	for _, key := range restored.Unresolved {
		w.printf("secret not resolved, left as it was: %s\n", key)
	}
	if len(restored.Actions) == 0 {
		w.printf("nothing to restore\n")
	}
	return cmp.Or(w.done(), outcome(blocked))
}
