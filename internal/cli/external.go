package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/antonL95/promptstor/internal/config"
)

var errNoConfig = errors.New("no configuration yet; run promptstor doctor first, it asks which files another tool writes")

func newExternalCommand(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "external",
		Short: "List the files and directories another tool or script manages",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "Print the external paths",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return editExternal(cmd, d, "", nil)
			},
		},
		&cobra.Command{
			Use:   "add <path>",
			Short: "Leave this file or directory to whoever else writes it",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return editExternal(cmd, d, args[0], addExternal)
			},
		},
		&cobra.Command{
			Use:   "remove <path>",
			Short: "Let PromptStor write this file or directory again",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return editExternal(cmd, d, args[0], removeExternal)
			},
		},
	)
	return cmd
}

func editExternal(cmd *cobra.Command, d Deps, arg string, edit func([]string, string) ([]string, error)) error {
	cfg, found, err := config.Load(d.Env.ConfigFile())
	if err != nil {
		return err
	}
	if edit != nil {
		if !found {
			return errNoConfig
		}
		path, err := externalPath(d, arg)
		if err != nil {
			return err
		}
		if cfg.External, err = edit(cfg.External, path); err != nil {
			return err
		}
		if err := cfg.Save(d.Env.ConfigFile()); err != nil {
			return err
		}
	}
	w := &printer{w: cmd.OutOrStdout()}
	if len(cfg.External) == 0 {
		w.printf("No external paths.\n")
	}
	for _, path := range cfg.External {
		w.printf("%s\n", path)
	}
	return w.done()
}

func externalPath(d Deps, arg string) (string, error) {
	path := d.Env.Expand(arg)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s: an external path must be absolute or start with ~/", arg)
	}
	return filepath.Clean(path), nil
}

func addExternal(list []string, path string) ([]string, error) {
	if !slices.Contains(list, path) {
		list = append(list, path)
	}
	slices.Sort(list)
	return list, nil
}

func removeExternal(list []string, path string) ([]string, error) {
	at := slices.Index(list, path)
	if at < 0 {
		return nil, fmt.Errorf("%s is not an external path", path)
	}
	return slices.Delete(list, at, at+1), nil
}
