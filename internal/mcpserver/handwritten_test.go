package mcpserver_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/testenv"
)

func writeRule(t *testing.T, layerDir string) string {
	t.Helper()
	path := filepath.Join(layerDir, "rules", "added-by-hand.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Name tests after behaviour.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func temporaryIDOf(t *testing.T, ids []library.ID) library.ID {
	t.Helper()
	at := slices.IndexFunc(ids, func(id library.ID) bool { return strings.HasPrefix(string(id), "ps_tmp_") })
	if at < 0 {
		t.Fatalf("no temporary id in %v", ids)
	}
	return ids[at]
}

func TestReadToolsNeverWriteIDs(t *testing.T) {
	env := home(t)
	writeRule(t, env.LibraryDir())
	cs := connect(t, newEngine(env), noCommands)
	before := testenv.TreeHash(t, env.Home)

	id := temporaryIDOf(t, listIDs(t, cs, map[string]any{}))
	for tool, args := range map[string]map[string]any{
		"promptstor_get":    {"id": string(id)},
		"promptstor_search": {"query": "behaviour"},
		"promptstor_status": {},
	} {
		if text, isError := call(t, cs, tool, args); isError {
			t.Errorf("%s: %s", tool, text)
		}
	}

	if testenv.TreeHash(t, env.Home) != before {
		t.Error("a read tool changed files")
	}
}

func TestUpsertByTemporaryIDStoresARealID(t *testing.T) {
	env := home(t)
	path := writeRule(t, env.LibraryDir())
	cs := connect(t, newEngine(env), noCommands)
	id := temporaryIDOf(t, listIDs(t, cs, map[string]any{}))

	var result changeResult
	mustCall(t, cs, "promptstor_upsert", map[string]any{
		"layer": "personal",
		"item":  map[string]any{"id": string(id), "kind": "rule", "name": "added-by-hand", "body": "Changed.\n"},
	}, &result)

	content := readFile(t, path)
	if strings.Contains(content, "ps_tmp_") || !strings.Contains(content, string(result.ID)) || !strings.Contains(content, "Changed.") {
		t.Errorf("result %s, item:\n%s", result.ID, content)
	}
}

func TestRemoveByTemporaryIDDeletesTheFile(t *testing.T) {
	env := home(t)
	path := writeRule(t, env.LibraryDir())
	cs := connect(t, newEngine(env), noCommands)
	id := temporaryIDOf(t, listIDs(t, cs, map[string]any{}))

	var result changeResult
	mustCall(t, cs, "promptstor_remove", map[string]any{"layer": "personal", "id": string(id)}, &result)

	if exists(path) {
		t.Error("the file is still there")
	}
}

func TestProjectChangeByTemporaryID(t *testing.T) {
	env := home(t)
	repo := newRepo(t)
	writeRule(t, filepath.Join(repo, ".promptstor"))
	git(t, repo, "add", "-A")
	git(t, repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "rule")
	var calls [][]string
	cs := connect(t, newEngine(env), gitRunner(t, &calls, nil))
	before := testenv.TreeHash(t, filepath.Join(repo, ".promptstor"))
	id := temporaryIDOf(t, listIDs(t, cs, map[string]any{"project_dir": repo}))

	var result changeResult
	mustCall(t, cs, "promptstor_upsert", map[string]any{
		"layer":       "project",
		"project_dir": repo,
		"item":        map[string]any{"id": string(id), "kind": "rule", "name": "added-by-hand", "body": "Changed.\n"},
	}, &result)

	committed := git(t, repo, "show", result.Branch+":.promptstor/rules/added-by-hand.md")
	if strings.Contains(committed, "ps_tmp_") || !strings.Contains(committed, string(result.ID)) || !strings.Contains(committed, "Changed.") {
		t.Errorf("result %s, committed:\n%s", result.ID, committed)
	}
	if testenv.TreeHash(t, filepath.Join(repo, ".promptstor")) != before {
		t.Error("the working tree layer changed")
	}
}
