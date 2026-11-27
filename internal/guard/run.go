package guard

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/manifest"
)

const maxPayload = 32 << 20

func Run(tool, manifestPath string, stdin io.Reader, stdout, stderr io.Writer) (code int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			code = failOpen(tool, fmt.Errorf("internal error: %v", recovered), stdout, stderr)
		}
	}()
	if !slices.Contains(tools, tool) {
		warn(stderr, fmt.Errorf("unknown tool %q", tool))
		return 0
	}
	payload, err := io.ReadAll(io.LimitReader(stdin, maxPayload))
	if err != nil {
		return failOpen(tool, fmt.Errorf("read payload: %w", err), stdout, stderr)
	}
	edits, err := Parse(tool, payload)
	if err != nil {
		return failOpen(tool, err, stdout, stderr)
	}
	if len(edits) == 0 {
		return Respond(tool, false, "", stdout, stderr)
	}
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return failOpen(tool, err, stdout, stderr)
	}
	block, reason := Decide(m, edits)
	return Respond(tool, block, reason, stdout, stderr)
}

func failOpen(tool string, err error, stdout, stderr io.Writer) int {
	warn(stderr, err)
	return Respond(tool, false, "", stdout, stderr)
}

func warn(stderr io.Writer, err error) {
	line := strings.Join(strings.Fields(err.Error()), " ")
	_, _ = fmt.Fprintf(stderr, "promptstor hook: %s; the edit is allowed\n", line)
}
