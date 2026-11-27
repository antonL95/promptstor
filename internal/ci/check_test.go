package ci_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/ci"
	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	secretValue = "PSFIXTURE-LITERAL-5Q2W"
	inSync      = "note: agents-md: example-docs: AGENTS.md holds rules only\n" +
		"note: agents-md: release-notes: AGENTS.md holds rules only\n" +
		"in sync\n"
)

func repo(t *testing.T) string {
	t.Helper()
	return testenv.Home(t, "testdata/repo").Home
}

func check(t *testing.T, root string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code, err := ci.Check(root, &out)
	if err != nil {
		t.Fatal(err)
	}
	return code, out.String()
}

func read(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func write(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replace(t *testing.T, root, name, old, new string) {
	t.Helper()
	content := read(t, root, name)
	if !strings.Contains(content, old) {
		t.Fatalf("%s does not contain %q", name, old)
	}
	write(t, root, name, strings.Replace(content, old, new, 1))
}

func wantAll(t *testing.T, out string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(out, part) {
			t.Errorf("output does not contain %q:\n%s", part, out)
		}
	}
}

func TestPassesWhenInSync(t *testing.T) {
	root := repo(t)
	before := testenv.TreeHash(t, root)

	code, out := check(t, root)

	if code != 0 || out != inSync {
		t.Fatalf("code %d, output:\n%s", code, out)
	}
	if testenv.TreeHash(t, root) != before {
		t.Error("check changed the repository")
	}
}

func TestFailsOnHandEditAndPrintsDiff(t *testing.T) {
	root := repo(t)
	replace(t, root, "AGENTS.md", "Write in plain language.", "Write however you like.")
	if err := os.Remove(filepath.Join(root, ".agents", "skills", "release-notes", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	before := testenv.TreeHash(t, root)

	code, out := check(t, root)

	if code != 1 {
		t.Fatalf("code %d, output:\n%s", code, out)
	}
	wantAll(t, out,
		"differs: AGENTS.md (md-section)\n--- AGENTS.md\n+++ AGENTS.md\n",
		"-Write however you like.\n",
		"+Write in plain language.\n",
		"missing: .agents/skills/release-notes/SKILL.md (file)\n",
		"+Group the commits by type.\n",
		"2 problems\n",
	)
	if strings.Contains(out, root) {
		t.Errorf("output holds the absolute root:\n%s", out)
	}
	if testenv.TreeHash(t, root) != before {
		t.Error("check changed the repository")
	}
}

func TestIgnoresTextOutsideOwnedUnits(t *testing.T) {
	root := repo(t)
	write(t, root, "AGENTS.md", "# Mine\n\n"+read(t, root, "AGENTS.md")+"\nMore of mine.\n")
	replace(t, root, ".mcp.json", `"mcpServers": {`, `"mcpServers": {"own": {"command": "own-server"},`)

	if code, out := check(t, root); code != 0 {
		t.Fatalf("code %d, output:\n%s", code, out)
	}
}

func TestNeverReadsHome(t *testing.T) {
	home := t.TempDir()
	write(t, home, ".config/promptstor/library/rules/personal.md",
		"---\npromptstor:\n  id: ps_00000000000000000000000C21\n---\nA personal rule.\n")
	write(t, home, ".config/promptstor/config.yaml", "not: [valid")
	write(t, home, ".codex/config.toml", "not = [valid")
	write(t, home, ".claude/settings.json", "{not valid")
	write(t, home, ".cursor/mcp.json", "{not valid")
	write(t, home, ".gemini/settings.json", "{not valid")
	for _, name := range []string{"HOME", "USERPROFILE"} {
		t.Setenv(name, home)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	before := testenv.TreeHash(t, home)

	code, out := check(t, repo(t))

	if code != 0 || out != inSync {
		t.Fatalf("code %d, output:\n%s", code, out)
	}
	if testenv.TreeHash(t, home) != before {
		t.Error("check wrote to the home folder")
	}
}

func TestUnknownToolRejected(t *testing.T) {
	root := repo(t)
	write(t, root, ".promptstor/config.yaml", "tools:\n  - claude-code\n  - not-a-tool\n")

	var out bytes.Buffer
	code, err := ci.Check(root, &out)

	if code != 2 || err == nil || !strings.Contains(err.Error(), `unknown tool "not-a-tool"`) {
		t.Fatalf("code %d, error %v", code, err)
	}
}

func TestUnusableLayerIsAnError(t *testing.T) {
	tests := map[string]struct {
		arrange func(t *testing.T, root string)
		want    string
	}{
		"no layer folder": {
			func(t *testing.T, root string) { remove(t, root, ".promptstor") },
			"no .promptstor/config.yaml",
		},
		"no config": {
			func(t *testing.T, root string) { remove(t, root, ".promptstor/config.yaml") },
			"no .promptstor/config.yaml",
		},
		"no tools": {
			func(t *testing.T, root string) { write(t, root, ".promptstor/config.yaml", "tools: []\n") },
			"lists no tools",
		},
		"symlinked layer folder": {
			func(t *testing.T, root string) {
				moved := filepath.Join(t.TempDir(), "layer")
				if err := os.Rename(filepath.Join(root, ".promptstor"), moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, filepath.Join(root, ".promptstor")); err != nil {
					t.Fatal(err)
				}
			},
			"symlinked",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := repo(t)
			test.arrange(t, root)
			before := testenv.TreeHash(t, root)

			var out bytes.Buffer
			code, err := ci.Check(root, &out)

			if code != 2 || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("code %d, error %v", code, err)
			}
			if testenv.TreeHash(t, root) != before {
				t.Error("check changed the repository")
			}
		})
	}
}

func remove(t *testing.T, root, name string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(name))); err != nil {
		t.Fatal(err)
	}
}

func TestBlockingResultFails(t *testing.T) {
	root := repo(t)
	write(t, root, ".promptstor/permissions/no-force-push.yaml",
		"promptstor:\n  id: ps_00000000000000000000000C31\ndeny_commands:\n  - git push --force\n")

	code, out := check(t, root)

	if code != 1 {
		t.Fatalf("code %d, output:\n%s", code, out)
	}
	wantAll(t, out, "blocked: cursor: no-force-push: deny command \"git push --force\" cannot be written")
}

func TestSymlinkedTargetIsNotFollowed(t *testing.T) {
	root := repo(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.Rename(filepath.Join(root, "AGENTS.md"), outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	code, out := check(t, root)

	if code != 1 {
		t.Fatalf("code %d, output:\n%s", code, out)
	}
	wantAll(t, out, "blocked: AGENTS.md: symlinked -> "+outside)
	if strings.Contains(out, "plain language") {
		t.Errorf("output holds the linked content:\n%s", out)
	}
}

func TestNeverPrintsSecretValues(t *testing.T) {
	tests := map[string]func(t *testing.T, root string){
		"typed into a committed file": func(t *testing.T, root string) {
			replace(t, root, ".mcp.json", "${EXAMPLE_TOKEN}", secretValue)
		},
		"literal in the layer": func(t *testing.T, root string) {
			replace(t, root, ".promptstor/mcp/example-docs.yaml", "EXAMPLE_TOKEN:\n    ref: env:EXAMPLE_TOKEN", "EXAMPLE_TOKEN: "+secretValue)
		},
	}
	for name, arrange := range tests {
		t.Run(name, func(t *testing.T) {
			root := repo(t)
			arrange(t, root)

			code, out := check(t, root)

			if code != 1 {
				t.Fatalf("code %d, output:\n%s", code, out)
			}
			wantAll(t, out, "differs: .mcp.json (/mcpServers/example-docs)\n")
			if strings.Contains(out, secretValue) {
				t.Error("output holds the secret value")
			}
		})
	}
}

func TestLiteralSecretInProjectHookFails(t *testing.T) {
	root := repo(t)
	write(t, root, ".promptstor/hooks/notify.yaml", "promptstor:\n  id: ps_00000000000000000000000C51\nname: Notify\nnative:\n  claude-code:\n    event: Stop\n    hooks:\n      - type: command\n        command: \"curl -s -H 'Authorization: Bearer "+secretValue+"' https://hooks.example.com/notify\"\n")

	code, out := check(t, root)

	if code != 1 {
		t.Fatalf("code %d, output:\n%s", code, out)
	}
	wantAll(t, out, "literal secret in Notify (hook); use an env placeholder\n")
	if strings.Contains(out, secretValue) {
		t.Errorf("output holds the secret value:\n%s", out)
	}
}

func TestPassesOnWhatSyncWrites(t *testing.T) {
	tools := []string{adapter.Antigravity, adapter.ClaudeCode, adapter.Codex, adapter.Cursor, adapter.GeminiCLI, adapter.OpenCode}
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, ".promptstor"), os.DirFS("testdata/repo/.promptstor")); err != nil {
		t.Fatal(err)
	}
	write(t, root, ".promptstor/config.yaml", "tools: ["+strings.Join(tools, ", ")+"]\n")
	write(t, root, ".promptstor/rules/go-files.md",
		"---\npromptstor:\n  id: ps_00000000000000000000000C41\n  scope:\n    - \"**/*.go\"\n---\nRun gofmt.\n")
	write(t, root, ".promptstor/commands/deploy-preview.md",
		"---\npromptstor:\n  id: ps_00000000000000000000000C42\ndescription: Deploy a preview\n---\nDeploy the preview.\n")
	write(t, root, ".promptstor/subagents/reviewer.md",
		"---\npromptstor:\n  id: ps_00000000000000000000000C43\ndescription: Review a change\n---\nReview the change.\n")

	env := testenv.Home(t, t.TempDir())
	e := engine.Engine{
		Env:      env,
		Config:   config.Config{Tools: tools},
		Adapters: adapter.All(),
		Now:      func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
	}
	result, _, err := e.Sync(root, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Written) == 0 {
		t.Fatal("sync wrote nothing")
	}

	code, out := check(t, root)

	if code != 0 {
		t.Fatalf("code %d, output:\n%s", code, out)
	}
}
