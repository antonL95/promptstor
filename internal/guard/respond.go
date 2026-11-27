package guard

import (
	"fmt"
	"io"
	"slices"
)

const exitBlock = 2

var tools = []string{ClaudeCode, Codex, GeminiCLI, Cursor}

func Respond(tool string, block bool, reason string, stdout, stderr io.Writer) int {
	switch {
	case !slices.Contains(tools, tool):
		return 0
	case block:
		_, _ = fmt.Fprintln(stderr, reason)
		return exitBlock
	case tool == Cursor:
		_, _ = fmt.Fprintln(stdout, "{}")
	}
	return 0
}
