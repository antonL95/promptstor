package guard

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
)

const exitBlock = 2

var tools = []string{ClaudeCode, Codex, GeminiCLI, Cursor, Antigravity}

type antigravityDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

func Respond(tool string, block bool, reason string, stdout, stderr io.Writer) int {
	switch {
	case tool == Antigravity:
		decision := antigravityDecision{Decision: "allow"}
		if block {
			decision = antigravityDecision{Decision: "deny", Reason: reason}
		}
		data, err := json.Marshal(decision)
		if err != nil {
			return 0
		}
		_, _ = fmt.Fprintf(stdout, "%s\n", data)
		return 0
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
