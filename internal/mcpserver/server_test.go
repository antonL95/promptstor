package mcpserver_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/mcpserver"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	ruleID    library.ID = "ps_0000000000000000000000MCP1"
	skillID   library.ID = "ps_0000000000000000000000MCP2"
	serverID  library.ID = "ps_0000000000000000000000MCP3"
	companyID library.ID = "ps_0000000000000000000000CMP1"
)

type runner func(dir, name string, args ...string) ([]byte, error)

type listed struct {
	ID      library.ID `json:"id"`
	Name    string     `json:"name"`
	Kind    string     `json:"kind"`
	Layer   string     `json:"layer"`
	Targets struct {
		Include []string `json:"include"`
		Exclude []string `json:"exclude"`
	} `json:"targets"`
}

type changeResult struct {
	Layer       string     `json:"layer"`
	ID          library.ID `json:"id"`
	Changed     []string   `json:"changed"`
	Snapshot    string     `json:"snapshot"`
	Branch      string     `json:"branch"`
	PullRequest string     `json:"pull_request"`
}

func home(t *testing.T) paths.Env {
	t.Helper()
	env := testenv.Home(t, "testdata/home")
	env.StateHome = filepath.Join(t.TempDir(), "state")
	return env
}

func newEngine(env paths.Env) engine.Engine {
	clock := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	return engine.Engine{
		Env:      env,
		Config:   config.Config{Version: 1, Tools: []string{adapter.ClaudeCode}},
		Adapters: adapter.All(),
		Now: func() time.Time {
			clock = clock.Add(time.Second)
			return clock
		},
	}
}

func noCommands(dir, name string, _ ...string) ([]byte, error) {
	return nil, errors.New("unexpected command " + name + " in " + dir)
}

func connect(t *testing.T, e engine.Engine, run runner) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	session, err := mcpserver.New(e, run).Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture-client", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		_ = session.Wait()
	})
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var text strings.Builder
	for _, content := range res.Content {
		if c, ok := content.(*mcp.TextContent); ok {
			text.WriteString(c.Text)
		}
	}
	return text.String(), res.IsError
}

func mustCall(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any, out any) {
	t.Helper()
	text, isError := call(t, cs, tool, args)
	if isError {
		t.Fatalf("%s returned a tool error: %s", tool, text)
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		t.Fatalf("%s: decode %q: %v", tool, text, err)
	}
}

func listIDs(t *testing.T, cs *mcp.ClientSession, args map[string]any) []library.ID {
	t.Helper()
	var entries []listed
	mustCall(t, cs, "promptstor_list", args, &entries)
	var ids []library.ID
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return ids
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func gitRunner(t *testing.T, calls *[][]string, fake func(name string, args []string) bool) runner {
	gitHome := t.TempDir()
	return func(dir, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, append([]string{name}, args...))
		if fake != nil && fake(name, args) {
			return []byte("https://example.invalid/pull/1\n"), nil
		}
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "HOME="+gitHome, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		return cmd.Output()
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	var calls [][]string
	out, err := gitRunner(t, &calls, nil)(dir, "git", args...)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("git %v: %v\n%s", args, err, exit.Stderr)
		}
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Fixture project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "init")
	return repo
}

func projectUpsert(repo string) map[string]any {
	return map[string]any{
		"layer":       "project",
		"project_dir": repo,
		"item":        map[string]any{"kind": "rule", "name": "Short answers", "body": "Answer briefly.\n"},
	}
}

func withGH(env paths.Env) paths.Env {
	env.LookPath = func(name string) (string, error) {
		if name == "gh" {
			return "/usr/local/bin/gh", nil
		}
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	return env
}

func pushed(calls [][]string) bool {
	return slices.ContainsFunc(calls, func(c []string) bool {
		return c[0] == "gh" || (c[0] == "git" && slices.Contains(c, "push"))
	})
}

func TestListFiltersByKind(t *testing.T) {
	cs := connect(t, newEngine(home(t)), noCommands)

	var entries []listed
	mustCall(t, cs, "promptstor_list", map[string]any{"kind": "skill"}, &entries)

	if len(entries) != 1 || entries[0].ID != skillID || entries[0].Name != "release-notes" || entries[0].Kind != "skill" || entries[0].Layer != "personal" {
		t.Fatalf("entries %+v", entries)
	}
	if got := listIDs(t, cs, map[string]any{}); !slices.Equal(got, []library.ID{ruleID, skillID, serverID}) {
		t.Errorf("all items %v", got)
	}
}

func TestListFiltersByLayerAndTool(t *testing.T) {
	e := newEngine(home(t))
	e.Builtins = []library.Item{{ID: companyID, Kind: library.KindRule, Name: "Company rule", Layer: library.LayerCompany, Body: "Company wording.\n", Targets: library.Targets{Include: []string{adapter.Codex}}}}
	cs := connect(t, e, noCommands)

	if got := listIDs(t, cs, map[string]any{"layer": "company"}); !slices.Equal(got, []library.ID{companyID}) {
		t.Errorf("company layer %v", got)
	}
	if got := listIDs(t, cs, map[string]any{"tool": adapter.ClaudeCode}); slices.Contains(got, companyID) || len(got) != 3 {
		t.Errorf("claude-code items %v", got)
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	cs := connect(t, newEngine(home(t)), noCommands)

	for query, want := range map[string][]library.ID{
		"PLAIN LANGUAGE":  {ruleID},
		"group them BY":   {skillID},
		"synthetic DOCUM": {serverID},
		"nothing matches": nil,
	} {
		text, isError := call(t, cs, "promptstor_search", map[string]any{"query": query})
		if isError {
			t.Fatalf("%q: %s", query, text)
		}
		var entries []listed
		if err := json.Unmarshal([]byte(text), &entries); err != nil {
			t.Fatal(err)
		}
		var got []library.ID
		for _, entry := range entries {
			got = append(got, entry.ID)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q found %v, want %v", query, got, want)
		}
		if strings.Contains(text, "EXAMPLE_TOKEN") || strings.Contains(text, "body") {
			t.Errorf("%q result carries item content: %s", query, text)
		}
	}
}

func TestGetReturnsFullItemWithStoredReferences(t *testing.T) {
	cs := connect(t, newEngine(home(t)), noCommands)

	var server map[string]any
	mustCall(t, cs, "promptstor_get", map[string]any{"name": "example-docs"}, &server)
	if server["id"] != string(serverID) || server["description"] != "Synthetic documentation server" {
		t.Fatalf("server %v", server)
	}
	mcpServer, _ := server["mcp"].(map[string]any)
	env, _ := mcpServer["env"].(map[string]any)
	if token, _ := env["EXAMPLE_TOKEN"].(map[string]any); token["ref"] != "env:EXAMPLE_TOKEN" {
		t.Errorf("env %v", env)
	}

	var rule map[string]any
	mustCall(t, cs, "promptstor_get", map[string]any{"id": string(ruleID)}, &rule)
	if rule["body"] != "Write in plain language.\n" || rule["layer"] != "personal" {
		t.Errorf("rule %v", rule)
	}
	if text, isError := call(t, cs, "promptstor_get", map[string]any{"id": "ps_missing"}); !isError || !strings.Contains(text, "ps_missing") {
		t.Errorf("missing item: %v %s", isError, text)
	}
}

func TestUpsertPersonalWritesAndSyncs(t *testing.T) {
	env := home(t)
	cs := connect(t, newEngine(env), noCommands)

	var result changeResult
	mustCall(t, cs, "promptstor_upsert", map[string]any{
		"layer": "personal",
		"item":  map[string]any{"kind": "rule", "name": "Short answers", "description": "Keep replies short", "body": "Answer briefly.\n"},
	}, &result)

	stored := filepath.Join(env.LibraryDir(), "rules", "short-answers.md")
	if !strings.Contains(readFile(t, stored), "Answer briefly.") {
		t.Errorf("library file %q", readFile(t, stored))
	}
	if !strings.HasPrefix(string(result.ID), "ps_") || !strings.Contains(readFile(t, stored), string(result.ID)) {
		t.Errorf("id %q", result.ID)
	}
	claude := filepath.Join(env.Home, ".claude", "CLAUDE.md")
	if !strings.Contains(readFile(t, claude), "Answer briefly.") || !strings.Contains(readFile(t, claude), "# Personal notes") {
		t.Errorf("CLAUDE.md %q", readFile(t, claude))
	}
	if result.Snapshot == "" || !slices.Contains(result.Changed, claude) || result.Layer != "personal" {
		t.Errorf("result %+v", result)
	}
	if !exists(env.ManifestPath()) {
		t.Error("sync left no manifest")
	}
}

func TestUpsertRenameDeletesOldFile(t *testing.T) {
	env := home(t)
	cs := connect(t, newEngine(env), noCommands)

	var result changeResult
	mustCall(t, cs, "promptstor_upsert", map[string]any{
		"layer": "personal",
		"item":  map[string]any{"id": string(ruleID), "kind": "rule", "name": "Simple wording", "body": "Use simple words.\n"},
	}, &result)

	if exists(filepath.Join(env.LibraryDir(), "rules", "plain-language.md")) {
		t.Error("old file still present")
	}
	renamed := readFile(t, filepath.Join(env.LibraryDir(), "rules", "simple-wording.md"))
	if !strings.Contains(renamed, string(ruleID)) || !strings.Contains(renamed, "Use simple words.") {
		t.Errorf("renamed file %q", renamed)
	}
	if result.ID != ruleID {
		t.Errorf("id %q", result.ID)
	}
}

func TestUpsertKeepsFieldsTheInputCannotCarry(t *testing.T) {
	env := home(t)
	cs := connect(t, newEngine(env), noCommands)

	var result changeResult
	mustCall(t, cs, "promptstor_upsert", map[string]any{
		"layer": "personal",
		"item":  map[string]any{"id": string(serverID), "kind": "mcp", "name": "example-docs", "description": "Renamed description"},
	}, &result)

	stored := readFile(t, filepath.Join(env.LibraryDir(), "mcp", "example-docs.yaml"))
	for _, want := range []string{"Renamed description", "command: example-docs-server", "ref: env:EXAMPLE_TOKEN"} {
		if !strings.Contains(stored, want) {
			t.Errorf("stored server lacks %q:\n%s", want, stored)
		}
	}
}

func TestUpsertValidationErrorIsToolError(t *testing.T) {
	env := home(t)
	before := testenv.TreeHash(t, env.Home)
	cs := connect(t, newEngine(env), noCommands)

	for name, args := range map[string]map[string]any{
		"bad skill name": {"layer": "personal", "item": map[string]any{"kind": "skill", "name": "Bad Skill Name", "body": "x\n"}},
		"unknown kind":   {"layer": "personal", "item": map[string]any{"kind": "widget", "name": "thing"}},
		"bad activation": {"layer": "personal", "item": map[string]any{"kind": "rule", "name": "Odd", "activation": "sometimes"}},
		"unknown layer":  {"layer": "global", "item": map[string]any{"kind": "rule", "name": "Odd"}},
	} {
		if text, isError := call(t, cs, "promptstor_upsert", args); !isError || text == "" {
			t.Errorf("%s: isError %v text %q", name, isError, text)
		}
	}
	if testenv.TreeHash(t, env.Home) != before {
		t.Error("a rejected upsert changed the home")
	}
}

func TestUpsertDeviceRejected(t *testing.T) {
	env := home(t)
	before := testenv.TreeHash(t, env.Home)
	cs := connect(t, newEngine(env), noCommands)
	item := map[string]any{"kind": "rule", "name": "Short answers", "body": "Answer briefly.\n"}

	for layer, want := range map[string]string{
		"device":  "agents may change only the personal layer",
		"company": "available with team accounts",
		"team":    "available with team accounts",
	} {
		text, isError := call(t, cs, "promptstor_upsert", map[string]any{"layer": layer, "item": item})
		if !isError || !strings.Contains(text, want) {
			t.Errorf("%s: isError %v text %q", layer, isError, text)
		}
		text, isError = call(t, cs, "promptstor_remove", map[string]any{"layer": layer, "id": string(ruleID)})
		if !isError || !strings.Contains(text, want) {
			t.Errorf("remove %s: isError %v text %q", layer, isError, text)
		}
	}
	if testenv.TreeHash(t, env.Home) != before || exists(env.StateDir()) {
		t.Error("a rejected change wrote something")
	}
}

func TestUpsertBuiltinRejected(t *testing.T) {
	env := home(t)
	e := newEngine(env)
	guard := library.EditGuard(adapter.ClaudeCode, "/opt/promptstor/bin/promptstor")
	e.Builtins = []library.Item{guard}
	cs := connect(t, e, noCommands)

	text, isError := call(t, cs, "promptstor_remove", map[string]any{"layer": "personal", "id": string(guard.ID)})
	if !isError || !strings.Contains(text, "built in") {
		t.Errorf("remove: isError %v text %q", isError, text)
	}
	text, isError = call(t, cs, "promptstor_upsert", map[string]any{"layer": "personal", "item": map[string]any{"id": string(guard.ID), "kind": "hook", "name": "edit guard claude-code"}})
	if !isError || !strings.Contains(text, "built in") {
		t.Errorf("upsert: isError %v text %q", isError, text)
	}
}

func TestRemoveInheritedWritesTombstone(t *testing.T) {
	env := home(t)
	e := newEngine(env)
	e.Builtins = []library.Item{{ID: companyID, Kind: library.KindRule, Name: "Company rule", Layer: library.LayerCompany, Body: "Company wording.\n"}}
	cs := connect(t, e, noCommands)

	var result changeResult
	mustCall(t, cs, "promptstor_remove", map[string]any{"layer": "personal", "id": string(companyID)}, &result)

	tombstone := readFile(t, filepath.Join(env.LibraryDir(), "rules", "company-rule.md"))
	if !strings.Contains(tombstone, "removed: true") || !strings.Contains(tombstone, string(companyID)) || strings.Contains(tombstone, "Company wording.") {
		t.Errorf("tombstone %q", tombstone)
	}
	if slices.Contains(listIDs(t, cs, map[string]any{}), companyID) {
		t.Error("removed item still listed")
	}
	if result.Layer != "personal" || !exists(env.ManifestPath()) {
		t.Errorf("remove did not sync: %+v", result)
	}
}

func TestRemovePersonalDeletesFile(t *testing.T) {
	env := home(t)
	cs := connect(t, newEngine(env), noCommands)

	var result changeResult
	mustCall(t, cs, "promptstor_remove", map[string]any{"layer": "personal", "id": string(skillID)}, &result)

	if exists(filepath.Join(env.LibraryDir(), "skills", "release-notes")) {
		t.Error("skill folder still present")
	}
	if slices.Contains(listIDs(t, cs, map[string]any{}), skillID) {
		t.Error("removed skill still listed")
	}
	if text, isError := call(t, cs, "promptstor_remove", map[string]any{"layer": "personal", "id": "ps_missing"}); !isError || !strings.Contains(text, "ps_missing") {
		t.Errorf("missing: isError %v text %q", isError, text)
	}
}

func TestStatusReturnsReportJSON(t *testing.T) {
	cs := connect(t, newEngine(home(t)), noCommands)

	var report struct {
		LibraryHash string           `json:"library_hash"`
		Findings    []map[string]any `json:"findings"`
	}
	mustCall(t, cs, "promptstor_status", map[string]any{}, &report)
	if report.LibraryHash == "" || len(report.Findings) == 0 {
		t.Errorf("report %+v", report)
	}
}

var branchName = regexp.MustCompile(`^promptstor/short-answers-\d{14}$`)

func TestUpsertProjectCreatesBranchWithCleanWorkingTree(t *testing.T) {
	env := home(t)
	repo := newRepo(t)
	homeBefore := testenv.TreeHash(t, env.Home)
	var calls [][]string
	cs := connect(t, newEngine(env), gitRunner(t, &calls, nil))

	var result changeResult
	mustCall(t, cs, "promptstor_upsert", projectUpsert(repo), &result)

	if !branchName.MatchString(result.Branch) || result.Layer != "project" {
		t.Fatalf("result %+v", result)
	}
	if status := git(t, repo, "status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Errorf("working tree not clean:\n%s", status)
	}
	if head := strings.TrimSpace(git(t, repo, "rev-parse", "--abbrev-ref", "HEAD")); head != "main" {
		t.Errorf("checked out %q", head)
	}
	files := strings.Fields(git(t, repo, "ls-tree", "-r", "--name-only", result.Branch))
	for _, want := range []string{".promptstor/rules/short-answers.md", "AGENTS.md", "README.md"} {
		if !slices.Contains(files, want) {
			t.Errorf("branch lacks %s: %v", want, files)
		}
	}
	agents := git(t, repo, "show", result.Branch+":AGENTS.md")
	if !strings.Contains(agents, "Answer briefly.") || strings.Contains(agents, "Write in plain language.") {
		t.Errorf("AGENTS.md on branch:\n%s", agents)
	}
	if log := git(t, repo, "log", "-1", "--format=%s|%an|%ae", result.Branch); log != "promptstor: update Short answers|PromptStor|promptstor@localhost\n" {
		t.Errorf("commit %q", log)
	}
	if worktrees := strings.Count(git(t, repo, "worktree", "list", "--porcelain"), "worktree "); worktrees != 1 {
		t.Errorf("%d worktrees left", worktrees)
	}
	if entries, _ := os.ReadDir(env.WorktreeDir()); len(entries) != 0 {
		t.Errorf("worktree folder holds %d entries", len(entries))
	}
	if exists(env.ManifestPath()) || exists(env.BackupDir()) {
		t.Error("project change touched the real manifest or backups")
	}
	if testenv.TreeHash(t, env.Home) != homeBefore {
		t.Error("project change touched the home")
	}
}

func TestUpsertProjectKeepsConfiguredAuthor(t *testing.T) {
	env := home(t)
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "Fixture Author")
	git(t, repo, "config", "user.email", "author@example.invalid")
	var calls [][]string
	cs := connect(t, newEngine(env), gitRunner(t, &calls, nil))

	var result changeResult
	mustCall(t, cs, "promptstor_upsert", projectUpsert(repo), &result)

	if log := git(t, repo, "log", "-1", "--format=%an|%ae", result.Branch); log != "Fixture Author|author@example.invalid\n" {
		t.Errorf("author %q", log)
	}
}

func TestRemoveProjectItemCreatesBranch(t *testing.T) {
	env := home(t)
	repo := newRepo(t)
	var calls [][]string
	cs := connect(t, newEngine(env), gitRunner(t, &calls, nil))
	var added changeResult
	mustCall(t, cs, "promptstor_upsert", projectUpsert(repo), &added)
	git(t, repo, "merge", "-q", "--ff-only", added.Branch)

	var removed changeResult
	mustCall(t, cs, "promptstor_remove", map[string]any{"layer": "project", "project_dir": repo, "id": string(added.ID)}, &removed)

	files := strings.Fields(git(t, repo, "ls-tree", "-r", "--name-only", removed.Branch))
	if slices.Contains(files, ".promptstor/rules/short-answers.md") {
		t.Errorf("branch still holds the rule: %v", files)
	}
	if status := git(t, repo, "status", "--porcelain"); status != "" {
		t.Errorf("working tree not clean:\n%s", status)
	}
}

func TestProjectChangeRejectsUnsafeID(t *testing.T) {
	env := home(t)
	repo := newRepo(t)
	var calls [][]string
	cs := connect(t, newEngine(env), gitRunner(t, &calls, nil))
	args := projectUpsert(repo)
	args["item"].(map[string]any)["id"] = "../../escaped"

	text, isError := call(t, cs, "promptstor_upsert", args)
	if !isError || !strings.Contains(text, "../../escaped") {
		t.Errorf("isError %v text %q", isError, text)
	}
	text, isError = call(t, cs, "promptstor_remove", map[string]any{"layer": "personal", "id": "../escaped"})
	if !isError || !strings.Contains(text, "../escaped") {
		t.Errorf("remove: isError %v text %q", isError, text)
	}
	if len(calls) != 0 || exists(env.StateDir()) {
		t.Errorf("an unsafe id reached git or the state folder: %v", calls)
	}
}

func TestNoPushWithoutOptIn(t *testing.T) {
	fake := func(name string, args []string) bool { return name == "gh" || slices.Contains(args, "push") }
	for name, setup := range map[string]func(*engine.Engine){
		"opt in off, gh present": func(e *engine.Engine) { e.Env = withGH(e.Env) },
		"opt in on, gh missing":  func(e *engine.Engine) { e.Config.OpenProjectPRs = true },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEngine(home(t))
			setup(&e)
			var calls [][]string
			cs := connect(t, e, gitRunner(t, &calls, fake))

			var result changeResult
			mustCall(t, cs, "promptstor_upsert", projectUpsert(newRepo(t)), &result)

			if pushed(calls) || result.PullRequest != "" {
				t.Errorf("pushed or opened a pull request: %v", calls)
			}
		})
	}
}

func TestPushAndDraftPullRequestWithOptIn(t *testing.T) {
	e := newEngine(home(t))
	e.Env = withGH(e.Env)
	e.Config.OpenProjectPRs = true
	fake := func(name string, args []string) bool { return name == "gh" || slices.Contains(args, "push") }
	var calls [][]string
	cs := connect(t, e, gitRunner(t, &calls, fake))

	var result changeResult
	mustCall(t, cs, "promptstor_upsert", projectUpsert(newRepo(t)), &result)

	push := slices.IndexFunc(calls, func(c []string) bool { return c[0] == "git" && slices.Contains(c, "push") })
	pr := slices.IndexFunc(calls, func(c []string) bool { return c[0] == "gh" })
	if push < 0 || pr < push || !slices.Contains(calls[push], result.Branch) {
		t.Fatalf("calls %v", calls)
	}
	if !slices.Contains(calls[pr], "--draft") || !slices.Contains(calls[pr], result.Branch) {
		t.Errorf("gh call %v", calls[pr])
	}
	if result.PullRequest != "https://example.invalid/pull/1" {
		t.Errorf("pull request %q", result.PullRequest)
	}
}
