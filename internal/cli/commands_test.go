package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	cliRuleID = library.ID("ps_0000000000000000000000CL11")
	cliSecret = "PSFIXTURE-KEYCHAIN-5T7W"
)

func cliHome(t *testing.T) paths.Env {
	t.Helper()
	env := testenv.Home(t, "testdata/home")
	env.StateHome = filepath.Join(t.TempDir(), "state")
	return env
}

func deps(env paths.Env) Deps {
	clock := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	return Deps{
		Stdin:  strings.NewReader(""),
		Stderr: io.Discard,
		Env:    env,
		Now: func() time.Time {
			clock = clock.Add(time.Minute)
			return clock
		},
		Run: func(name string, _ ...string) ([]byte, error) {
			return nil, errors.New("unexpected command " + name)
		},
	}
}

func run(t *testing.T, d Deps, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	d.Stdout = &out
	cmd := NewRoot(d)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func mustRun(t *testing.T, d Deps, args ...string) string {
	t.Helper()
	out, err := run(t, d, args...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return out
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

var snapshotLine = regexp.MustCompile(`(?m)^snapshot (\S+)$`)
var preRestoreLine = regexp.MustCompile(`(?m)^pre-restore snapshot (\S+)`)

func TestSyncDryRunWritesNothing(t *testing.T) {
	env := testenv.Home(t, "testdata/home")
	before := testenv.TreeHash(t, env.Home)

	out := mustRun(t, deps(env), "sync", "--dry-run")

	if testenv.TreeHash(t, env.Home) != before {
		t.Error("dry run changed the home")
	}
	for _, want := range []string{
		"--- " + filepath.Join(env.Home, ".claude", "CLAUDE.md"),
		"+++ " + filepath.Join(env.Home, ".claude", "skills", "release-notes", "SKILL.md"),
		"+Write in plain language.",
		" Keep answers short.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !regexp.MustCompile(`(?m)^\d+ files to change, 0 blocked, 0 unsupported$`).MatchString(out) {
		t.Errorf("no summary:\n%s", out)
	}
	if strings.Contains(out, "snapshot ") {
		t.Errorf("dry run applied:\n%s", out)
	}
}

func TestSyncAppliesAndPrintsSnapshot(t *testing.T) {
	env := cliHome(t)

	out := mustRun(t, deps(env), "sync")

	if !snapshotLine.MatchString(out) {
		t.Errorf("no snapshot id:\n%s", out)
	}
	skill := filepath.Join(env.Home, ".claude", "skills", "release-notes", "SKILL.md")
	if !strings.Contains(out, "wrote "+skill) {
		t.Errorf("written file not listed:\n%s", out)
	}
	if _, err := os.Stat(skill); err != nil {
		t.Error(err)
	}
}

func TestSyncBlockedFileExitsNonZero(t *testing.T) {
	env := cliHome(t)
	skill := filepath.Join(env.Home, ".claude", "skills", "release-notes", "SKILL.md")
	writeFile(t, skill, "hand written\n")

	out, err := run(t, deps(env), "sync", "--dry-run")

	if err == nil {
		t.Error("blocked sync exited zero")
	}
	if !strings.Contains(out, "blocked:\n  "+skill+": not created by promptstor") {
		t.Errorf("blocked file not printed:\n%s", out)
	}
	if !strings.Contains(out, ", 1 blocked,") {
		t.Errorf("summary:\n%s", out)
	}
}

func TestSyncPrintsUnsupportedByToolAndItem(t *testing.T) {
	env := cliHome(t)
	writeFile(t, filepath.Join(env.LibraryDir(), "mcp", "example-docs.yaml"), "promptstor:\n  id: ps_0000000000000000000000CL13\ntransport: stdio\ncommand: example-docs-server\nenv:\n  EXAMPLE_TOKEN:\n    ref: keychain:example-service/example-account\n")

	out := mustRun(t, deps(env), "sync", "--dry-run")

	want := "unsupported:\n  claude-code\n    example-docs: needs manual setup: env/EXAMPLE_TOKEN\n  codex\n    example-docs: needs manual setup: env/EXAMPLE_TOKEN\n"
	if !strings.Contains(out, want) {
		t.Errorf("output lacks\n%s\ngot\n%s", want, out)
	}
}

func TestSyncUsesInjectedRunnerForSecrets(t *testing.T) {
	env := cliHome(t)
	writeFile(t, filepath.Join(env.LibraryDir(), "mcp", "example-docs.yaml"), "promptstor:\n  id: ps_0000000000000000000000CL13\ntransport: stdio\ncommand: example-docs-server\nenv:\n  EXAMPLE_TOKEN:\n    ref: keychain:example-service/example-account\n")
	writeFile(t, env.ConfigFile(), "version: 1\ntools:\n  - claude-code\nresolve_secrets:\n  - example-docs\n")
	d := deps(env)
	var calls []string
	d.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return []byte(cliSecret + "\n"), nil
	}

	out := mustRun(t, d, "sync", "--dry-run")

	if !slices.Contains(calls, "security find-generic-password -s example-service -a example-account -w") {
		t.Errorf("calls %v", calls)
	}
	if strings.Contains(out, "unsupported:") {
		t.Errorf("secret not resolved:\n%s", out)
	}
}

func TestSyncProjectFlagResolvesRelativePath(t *testing.T) {
	env := cliHome(t)
	project := t.TempDir()
	t.Chdir(project)

	out := mustRun(t, deps(env), "sync", "--dry-run", "--project", ".")

	if !strings.Contains(out, "+++ "+filepath.Join(project, "AGENTS.md")) {
		t.Errorf("project AGENTS.md not planned:\n%s", out)
	}
}

func TestLockViolationPrinted(t *testing.T) {
	env := cliHome(t)
	d := deps(env)
	d.Builtins = []library.Item{{ID: cliRuleID, Kind: library.KindRule, Name: "Plain language", Layer: library.LayerCompany, Locked: true, Body: "Company wording.\n"}}

	out, err := run(t, d, "sync", "--dry-run")

	if err == nil {
		t.Error("lock violation exited zero")
	}
	want := "locked:\n  Plain language (" + string(cliRuleID) + "): the personal layer cannot change it, locked by company\n"
	if !strings.Contains(out, want) {
		t.Errorf("output lacks\n%s\ngot\n%s", want, out)
	}
}

func TestRestoreListNewestFirst(t *testing.T) {
	env := cliHome(t)
	d := deps(env)
	first := snapshotLine.FindStringSubmatch(mustRun(t, d, "sync"))[1]
	pre := preRestoreLine.FindStringSubmatch(mustRun(t, d, "restore", first))[1]

	out := mustRun(t, d, "restore", "--list")

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("list:\n%s", out)
	}
	if !strings.HasPrefix(lines[0], pre+"  ") || !strings.Contains(lines[0], "  pre-restore  ") {
		t.Errorf("first line %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], first+"  2026-11-27T10:") || !strings.Contains(lines[1], "  change  ") || !strings.HasSuffix(lines[1], " files") {
		t.Errorf("second line %q", lines[1])
	}
}

func TestRestoreListEmpty(t *testing.T) {
	if out := mustRun(t, deps(cliHome(t)), "restore", "--list"); out != "no snapshots\n" {
		t.Errorf("got %q", out)
	}
}

func TestRestoreNeedsSnapshotID(t *testing.T) {
	if _, err := run(t, deps(cliHome(t)), "restore"); err == nil {
		t.Error("restore without an id accepted")
	}
}

func TestRestoreCanBeUndoneViaPreRestore(t *testing.T) {
	env := cliHome(t)
	d := deps(env)
	original := testenv.TreeHash(t, env.Home)
	change := snapshotLine.FindStringSubmatch(mustRun(t, d, "sync"))[1]
	synced := testenv.TreeHash(t, env.Home)

	dry := mustRun(t, d, "restore", change, "--dry-run")
	if testenv.TreeHash(t, env.Home) != synced || !strings.Contains(dry, "delete ") {
		t.Fatalf("dry run restore:\n%s", dry)
	}

	out := mustRun(t, d, "restore", change)
	if testenv.TreeHash(t, env.Home) != original {
		t.Fatalf("restore did not return the original tree:\n%s", out)
	}
	pre := preRestoreLine.FindStringSubmatch(out)
	if pre == nil {
		t.Fatalf("no pre-restore snapshot:\n%s", out)
	}

	mustRun(t, d, "restore", pre[1])
	if testenv.TreeHash(t, env.Home) != synced {
		t.Error("pre-restore snapshot did not bring the synced tree back")
	}
}

func TestStatusWithoutCheck(t *testing.T) {
	env := cliHome(t)

	out := mustRun(t, deps(env), "status")

	for _, want := range []string{"no check has run yet\n", "tools: claude-code, codex\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !regexp.MustCompile(`(?m)^library: [0-9a-f]{64}$`).MatchString(out) {
		t.Errorf("no library hash:\n%s", out)
	}
}

func TestStatusCountsFindings(t *testing.T) {
	env := cliHome(t)
	writeFile(t, env.LastCheckPath(), `{"checked":"2026-11-27T09:30:00Z","library_hash":"0f3c9a","clean":false,"fingerprint":"4e1d","counts":{"pending":2,"info":1}}`)

	out := mustRun(t, deps(env), "status")

	want := "last check: 2026-11-27T09:30:00Z\n  info: 1\n  pending: 2\n"
	if !strings.Contains(out, want) {
		t.Errorf("output lacks\n%s\ngot\n%s", want, out)
	}
}

func TestUnifiedDiff(t *testing.T) {
	before := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\ntwelve\n"
	after := "one\ntwo\nthree\nFOUR\nfive\nsix\nseven\neight\nnine\nten\neleven\ntwelve\nthirteen"
	want := "--- /x/file\n+++ /x/file\n" +
		"@@ -1,7 +1,7 @@\n one\n two\n three\n-four\n+FOUR\n five\n six\n seven\n" +
		"@@ -10,3 +10,4 @@\n ten\n eleven\n twelve\n+thirteen\n\\ No newline at end of file\n"
	if got := unifiedDiff("/x/file", []byte(before), []byte(after)); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestUnifiedDiffNewFile(t *testing.T) {
	want := "--- /x/file\n+++ /x/file\n@@ -0,0 +1,2 @@\n+a\n+b\n"
	if got := unifiedDiff("/x/file", nil, []byte("a\nb\n")); got != want {
		t.Errorf("got\n%s", got)
	}
}

func TestUnifiedDiffDeletedFile(t *testing.T) {
	want := "--- /x/file\n+++ /x/file\n@@ -1,1 +0,0 @@\n-a\n"
	if got := unifiedDiff("/x/file", []byte("a\n"), nil); got != want {
		t.Errorf("got\n%s", got)
	}
}

func TestSyncNeverPrintsSecretsOrNeighbouringValues(t *testing.T) {
	env := cliHome(t)
	neighbour := "PSFIXTURE-NEIGHBOUR-2H6D"
	writeFile(t, filepath.Join(env.Home, ".claude.json"), "{\n  \"mcpServers\": {\n    \"hand-written\": {\n      \"type\": \"http\",\n      \"url\": \"https://mcp.example.com\",\n      \"headers\": {\"Authorization\": \"Bearer "+neighbour+"\"}\n    }\n  }\n}\n")
	writeFile(t, filepath.Join(env.LibraryDir(), "mcp", "example-docs.yaml"), "promptstor:\n  id: ps_0000000000000000000000CL13\ntransport: stdio\ncommand: example-docs-server\nenv:\n  EXAMPLE_TOKEN:\n    ref: keychain:example-service/example-account\n")
	writeFile(t, env.ConfigFile(), "version: 1\ntools:\n  - claude-code\n  - codex\nresolve_secrets:\n  - example-docs\n")
	d := deps(env)
	d.Run = func(string, ...string) ([]byte, error) { return []byte(cliSecret + "\n"), nil }

	for _, args := range [][]string{{"sync", "--dry-run"}, {"sync"}} {
		out := mustRun(t, d, args...)
		if strings.Contains(out, cliSecret) || strings.Contains(out, neighbour) {
			t.Fatalf("%v printed a secret value", args)
		}
		if len(args) == 2 && !strings.Contains(out, "@@ set /mcpServers/example-docs @@\n(secret, not shown)\n") {
			t.Errorf("secret unit not summarised:\n%s", out)
		}
	}
}
