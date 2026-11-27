package cli

import (
	"os"
	"strings"
	"testing"
)

func TestAdapterDocsUpToDate(t *testing.T) {
	committed, err := os.ReadFile("../../docs/adapters.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := mustRun(t, Deps{}, "adapters", "--markdown"); got != string(committed) {
		t.Errorf("docs/adapters.md is out of date; regenerate it with: go run ./cmd/promptstor adapters --markdown > docs/adapters.md\n--- got\n%s", got)
	}
}

func TestAdapterDocsDescribeEveryTool(t *testing.T) {
	out := mustRun(t, Deps{}, "adapters", "--markdown")
	for _, want := range []string{
		"## claude-code",
		"## codex",
		"## gemini-cli",
		"## antigravity",
		"## opencode",
		"## cursor",
		"## agents-md",
		"`~/.claude.json` (only `/mcpServers`)",
		"`~/.gemini/antigravity-cli`",
		"`~/.config/opencode`",
		"`cursor-agent`",
		"`{env:NAME}`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "/promptstor-docs") {
		t.Errorf("the placeholder folders leak into the output:\n%s", out)
	}
}

func TestAdaptersPrintsAShortTable(t *testing.T) {
	out := mustRun(t, Deps{}, "adapters")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 8 {
		t.Fatalf("want a header and seven tools, got %q", out)
	}
	if !strings.HasPrefix(lines[0], "TOOL") || strings.Contains(out, "|") {
		t.Errorf("not a plain table: %q", out)
	}
}
