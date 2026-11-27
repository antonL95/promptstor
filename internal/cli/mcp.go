package cli

import (
	"errors"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/antonL95/promptstor/internal/mcpserver"
)

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func newMCPCommand(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the library to agents over MCP on stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := d.engine()
			if err != nil {
				return err
			}
			transport := &mcp.IOTransport{Reader: io.NopCloser(cmd.InOrStdin()), Writer: nopWriteCloser{cmd.OutOrStdout()}}
			err = mcpserver.New(e, d.runIn).Run(cmd.Context(), transport)
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		},
	}
}

func (d Deps) runIn(dir, name string, args ...string) ([]byte, error) {
	if d.RunIn == nil {
		return nil, errNoRunner
	}
	return d.RunIn(dir, name, args...)
}
