package plan_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/adapter/fake"
	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/structedit"
	"github.com/antonL95/promptstor/internal/testenv"
)

var written = time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

type recorder struct {
	t       *testing.T
	root    string
	calls   int
	reason  string
	entries []plan.SnapshotEntry
	treeAt  string
	err     error
}

func (r *recorder) Snapshot(reason string, entries []plan.SnapshotEntry) (string, error) {
	r.calls++
	r.reason = reason
	r.entries = entries
	r.treeAt = testenv.TreeHash(r.t, r.root)
	if r.err != nil {
		return "", r.err
	}
	return "snap-1", nil
}

func home(t *testing.T) paths.Env {
	t.Helper()
	env := testenv.Home(t, "testdata/home")
	env.StateHome = filepath.Join(t.TempDir(), "state")
	return env
}

func at(env paths.Env, rel string) string {
	return filepath.Join(env.Home, filepath.FromSlash(rel))
}

func unit(env paths.Env, tool, rel string, kind manifest.UnitKind, key, content string) plan.Unit {
	return plan.Unit{
		Tool:    tool,
		Path:    at(env, rel),
		Root:    env.Home,
		Kind:    kind,
		Key:     key,
		Content: []byte(content),
		Mode:    0o644,
		Items:   []library.ID{library.ID("item-" + tool)},
	}
}

func export(t *testing.T, env paths.Env, adapters ...*fake.Adapter) []plan.Unit {
	t.Helper()
	var units []plan.Unit
	for _, a := range adapters {
		got, _, err := a.Export(adapter.Target{Env: env, Scope: adapter.ScopeUser}, nil)
		if err != nil {
			t.Fatal(err)
		}
		units = append(units, got...)
	}
	return units
}

func entryFor(t *testing.T, u plan.Unit) manifest.Entry {
	t.Helper()
	hash, err := manifest.Hash(u.Kind, u.Content)
	if err != nil {
		t.Fatal(err)
	}
	return manifest.Entry{Tool: u.Tool, Path: u.Path, Root: u.Root, Unit: u.Kind, Key: u.Key, Hash: hash, Items: u.Items, Written: written}
}

func diff(t *testing.T, desired []plan.Unit, m manifest.Manifest, tools []string, opt plan.Options) []plan.FileChange {
	t.Helper()
	changes, err := plan.Diff(desired, m, tools, opt)
	if err != nil {
		t.Fatal(err)
	}
	return changes
}

func only(t *testing.T, changes []plan.FileChange, path string) plan.FileChange {
	t.Helper()
	for _, c := range changes {
		if c.Path == path {
			return c
		}
	}
	t.Fatalf("no change for %s in %+v", path, changes)
	return plan.FileChange{}
}

func apply(env paths.Env, changes []plan.FileChange, snap plan.Snapshotter, m *manifest.Manifest) (plan.ApplyResult, error) {
	held, err := fsx.Hold(env.LockPath(), 0)
	if err != nil {
		return plan.ApplyResult{}, err
	}
	defer held.Release()
	a := plan.Applier{Now: func() time.Time { return written }}
	return a.Apply(held, changes, snap, m, env.ManifestPath())
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, target, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func loadManifest(t *testing.T, env paths.Env) manifest.Manifest {
	t.Helper()
	m, err := manifest.Load(env.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func assertUntouched(t *testing.T, env paths.Env, before string, snap *recorder) {
	t.Helper()
	if got := testenv.TreeHash(t, env.Home); got != before {
		t.Error("home changed")
	}
	if snap.calls != 0 {
		t.Errorf("snapshot called %d times", snap.calls)
	}
}

func matchingUnits(env paths.Env) []plan.Unit {
	return []plan.Unit{
		unit(env, adapter.ClaudeCode, ".claude/CLAUDE.md", manifest.UnitSection, "", "Use tabs.\n"),
		unit(env, adapter.ClaudeCode, ".claude/settings.json", manifest.UnitJSON, "/model", `"example-model"`),
		unit(env, adapter.ClaudeCode, ".claude/settings.json", manifest.UnitJSONElement, "/permissions/allow@=Bash(ls)", `"Bash(ls)"`),
		unit(env, adapter.Codex, ".codex/config.toml", manifest.UnitTOMLTable, "mcp_servers.docs", "[mcp_servers.docs]\ncommand = \"docs-server\"\n"),
	}
}

func TestDiffNoopWhenHashesMatch(t *testing.T) {
	env := home(t)
	desired := matchingUnits(env)
	var m manifest.Manifest
	for _, u := range desired {
		m.Put(entryFor(t, u))
	}
	changes := diff(t, desired, m, []string{adapter.ClaudeCode, adapter.Codex}, plan.Options{})
	if len(changes) != 3 {
		t.Fatalf("changes: %+v", changes)
	}
	for _, c := range changes {
		if c.Op != plan.OpNoop || c.Blocked != "" || len(c.Record) != 0 || string(c.After) != string(c.Before) || !c.Exists {
			t.Errorf("%s: op %s blocked %q record %d", c.Path, c.Op, c.Blocked, len(c.Record))
		}
	}
	before := testenv.TreeHash(t, env.Home)
	snap := &recorder{t: t, root: env.Home}
	result, err := apply(env, changes, snap, &m)
	if err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, env, before, snap)
	if len(result.Written) != 0 || len(result.Skipped) != 0 || result.SnapshotID != "" {
		t.Errorf("result %+v", result)
	}
}

func TestDiffAdoptsMatchingUnitsWithoutEntries(t *testing.T) {
	env := home(t)
	desired := matchingUnits(env)
	changes := diff(t, desired, manifest.Manifest{}, []string{adapter.ClaudeCode, adapter.Codex}, plan.Options{})
	recorded := 0
	for _, c := range changes {
		if c.Op != plan.OpNoop || c.Blocked != "" {
			t.Errorf("%s: op %s blocked %q", c.Path, c.Op, c.Blocked)
		}
		recorded += len(c.Record)
	}
	if recorded != len(desired) {
		t.Fatalf("recorded %d units, want %d", recorded, len(desired))
	}
	before := testenv.TreeHash(t, env.Home)
	snap := &recorder{t: t, root: env.Home}
	var m manifest.Manifest
	if _, err := apply(env, changes, snap, &m); err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, env, before, snap)
	saved := loadManifest(t, env)
	for _, u := range desired {
		got, ok := saved.Get(u.Path, u.Kind, u.Key)
		want := entryFor(t, u)
		want.Adopted = true
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("entry %s %s: got %+v want %+v", u.Path, u.Key, got, want)
		}
	}
}

func TestDiffBlocksSymlinkedFile(t *testing.T) {
	env := home(t)
	write(t, at(env, "dotfiles/CLAUDE.md"), "linked rules\n")
	path := at(env, ".claude/CLAUDE.md")
	link(t, filepath.Join("..", "dotfiles", "CLAUDE.md"), path)
	desired := []plan.Unit{unit(env, adapter.ClaudeCode, ".claude/CLAUDE.md", manifest.UnitSection, "", "Use spaces.\n")}

	changes := diff(t, desired, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{})
	c := only(t, changes, path)
	if want := "symlinked -> " + filepath.Join("..", "dotfiles", "CLAUDE.md"); c.Blocked != want {
		t.Errorf("blocked %q, want %q", c.Blocked, want)
	}
	before := testenv.TreeHash(t, env.Home)
	snap := &recorder{t: t, root: env.Home}
	var m manifest.Manifest
	result, err := apply(env, changes, snap, &m)
	if err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, env, before, snap)
	if len(result.Skipped) != 1 || result.Skipped[0].Path != path || len(result.Written) != 0 {
		t.Errorf("result %+v", result)
	}
	if len(m.Entries) != 0 {
		t.Errorf("manifest %+v", m.Entries)
	}
}

func TestDiffBlocksSymlinkedParentDir(t *testing.T) {
	env := home(t)
	if err := os.MkdirAll(at(env, "dotfiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(at(env, ".claude"), at(env, "dotfiles/claude")); err != nil {
		t.Fatal(err)
	}
	link(t, filepath.Join("dotfiles", "claude"), at(env, ".claude"))
	path := at(env, ".claude/settings.json")
	desired := []plan.Unit{
		unit(env, adapter.ClaudeCode, ".claude/settings.json", manifest.UnitJSON, "/model", `"other-model"`),
		unit(env, adapter.ClaudeCode, ".claude/skills/x/SKILL.md", manifest.UnitFile, "", "new skill\n"),
	}
	for _, opt := range []plan.Options{{}, {ReplaceSymlinks: []string{path}}} {
		changes := diff(t, desired, manifest.Manifest{}, []string{adapter.ClaudeCode}, opt)
		if len(changes) != 2 {
			t.Fatalf("changes %+v", changes)
		}
		for _, c := range changes {
			if !strings.HasPrefix(c.Blocked, "symlinked -> "+filepath.Join("dotfiles", "claude")) {
				t.Errorf("%s blocked %q", c.Path, c.Blocked)
			}
		}
		before := testenv.TreeHash(t, env.Home)
		snap := &recorder{t: t, root: env.Home}
		var m manifest.Manifest
		if _, err := apply(env, changes, snap, &m); err != nil {
			t.Fatal(err)
		}
		assertUntouched(t, env, before, snap)
	}
}

func TestReplaceSymlinkWithConsent(t *testing.T) {
	env := home(t)
	target := at(env, "dotfiles/CLAUDE.md")
	write(t, target, "linked rules\n")
	path := at(env, ".claude/CLAUDE.md")
	link(t, filepath.Join("..", "dotfiles", "CLAUDE.md"), path)
	desired := []plan.Unit{unit(env, adapter.ClaudeCode, ".claude/CLAUDE.md", manifest.UnitSection, "", "Use spaces.\n")}

	changes := diff(t, desired, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{ReplaceSymlinks: []string{path}})
	c := only(t, changes, path)
	if c.Blocked != "" || c.Op != plan.OpUpdate || !c.ReplaceLink || string(c.Before) != "linked rules\n" {
		t.Fatalf("change %+v", c)
	}
	snap := &recorder{t: t, root: env.Home}
	var m manifest.Manifest
	result, err := apply(env, changes, snap, &m)
	if err != nil {
		t.Fatal(err)
	}
	if want := []plan.SnapshotEntry{{Tool: adapter.ClaudeCode, Path: path, Root: env.Home}}; !reflect.DeepEqual(snap.entries, want) {
		t.Errorf("snapshot entries %+v", snap.entries)
	}
	entry, err := fsx.Inspect(path)
	if err != nil || entry.IsLink || !entry.Exists {
		t.Fatalf("path is still a link: %+v %v", entry, err)
	}
	if got := read(t, path); got != string(c.After) || !strings.HasPrefix(got, "linked rules\n") || !strings.Contains(got, "Use spaces.\n") {
		t.Errorf("content %q", got)
	}
	if got := read(t, target); got != "linked rules\n" {
		t.Errorf("link target changed: %q", got)
	}
	if !reflect.DeepEqual(result.Written, []string{path}) {
		t.Errorf("written %v", result.Written)
	}
}

func TestDiffSharedFileConflict(t *testing.T) {
	env := home(t)
	codex := &fake.Adapter{Tool: adapter.Codex, Units: []plan.Unit{
		unit(env, adapter.Codex, ".claude/settings.json", manifest.UnitJSON, "/model", `"model-a"`),
	}}
	gemini := &fake.Adapter{Tool: adapter.GeminiCLI, Units: []plan.Unit{
		unit(env, adapter.GeminiCLI, ".claude/settings.json", manifest.UnitJSON, "/model", `"model-b"`),
		unit(env, adapter.GeminiCLI, ".claude/settings.json", manifest.UnitJSON, "/theme", `"dark"`),
	}}
	path := at(env, ".claude/settings.json")
	changes := diff(t, export(t, env, gemini, codex), manifest.Manifest{}, []string{adapter.Codex, adapter.GeminiCLI}, plan.Options{})
	c := only(t, changes, path)
	if c.Blocked != "shared file conflict: codex, gemini-cli" {
		t.Errorf("blocked %q", c.Blocked)
	}
	before := testenv.TreeHash(t, env.Home)
	snap := &recorder{t: t, root: env.Home}
	var m manifest.Manifest
	result, err := apply(env, changes, snap, &m)
	if err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, env, before, snap)
	if len(result.Skipped) != 1 {
		t.Errorf("skipped %+v", result.Skipped)
	}
}

func TestDiffIdenticalSharedUnitsMerge(t *testing.T) {
	env := home(t)
	content := "---\nname: x\n---\nDo x.\n"
	claude := &fake.Adapter{Tool: adapter.ClaudeCode, Units: []plan.Unit{
		unit(env, adapter.ClaudeCode, ".agents/skills/x/SKILL.md", manifest.UnitFile, "", content),
	}}
	codex := &fake.Adapter{Tool: adapter.Codex, Units: []plan.Unit{
		unit(env, adapter.Codex, ".agents/skills/x/SKILL.md", manifest.UnitFile, "", content),
	}}
	path := at(env, ".agents/skills/x/SKILL.md")
	changes := diff(t, export(t, env, codex, claude), manifest.Manifest{}, []string{adapter.ClaudeCode, adapter.Codex}, plan.Options{})
	if len(changes) != 1 {
		t.Fatalf("changes %+v", changes)
	}
	c := only(t, changes, path)
	if c.Blocked != "" || c.Op != plan.OpCreate || len(c.Units) != 1 {
		t.Fatalf("change %+v", c)
	}
	if got := c.Units[0]; got.Tool != "claude-code,codex" || !reflect.DeepEqual(got.Items, []library.ID{"item-claude-code", "item-codex"}) {
		t.Errorf("merged unit %+v", got)
	}
	var m manifest.Manifest
	if _, err := apply(env, changes, &recorder{t: t, root: env.Home}, &m); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != content {
		t.Errorf("content %q", got)
	}
	saved := loadManifest(t, env)
	entry, ok := saved.Get(path, manifest.UnitFile, "")
	if !ok || entry.Tool != "claude-code,codex" {
		t.Fatalf("entry %+v", entry)
	}
	for _, tool := range []string{adapter.ClaudeCode, adapter.Codex} {
		if got := saved.ForTool(tool); len(got) != 1 {
			t.Errorf("ForTool(%s) = %+v", tool, got)
		}
	}
}

func TestDiffUnmanagedExistingFileBlocked(t *testing.T) {
	env := home(t)
	generated := at(env, "notes/generated.md")
	write(t, generated, string(managed.WithNotice([]byte("Old.\n"))))
	desired := []plan.Unit{
		unit(env, adapter.ClaudeCode, "notes/user.md", manifest.UnitFile, "", "Managed.\n"),
		unit(env, adapter.ClaudeCode, "notes/generated.md", manifest.UnitFile, "", string(managed.WithNotice([]byte("New.\n")))),
	}
	changes := diff(t, desired, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{})
	if c := only(t, changes, at(env, "notes/user.md")); c.Blocked != "not created by promptstor" {
		t.Errorf("hand written file: blocked %q", c.Blocked)
	}
	if c := only(t, changes, generated); c.Blocked != "" || c.Op != plan.OpUpdate {
		t.Errorf("file with notice: %+v", c)
	}
	var m manifest.Manifest
	result, err := apply(env, changes, &recorder{t: t, root: env.Home}, &m)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, at(env, "notes/user.md")); got != "Written by hand.\n" {
		t.Errorf("hand written file changed: %q", got)
	}
	if !reflect.DeepEqual(result.Written, []string{generated}) {
		t.Errorf("written %v", result.Written)
	}
}

func TestDiffAdoptAllowsOverwrite(t *testing.T) {
	env := home(t)
	path := at(env, "notes/user.md")
	desired := []plan.Unit{unit(env, adapter.ClaudeCode, "notes/user.md", manifest.UnitFile, "", "Managed.\n")}
	changes := diff(t, desired, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{Adopt: []string{path}})
	c := only(t, changes, path)
	if c.Blocked != "" || c.Op != plan.OpUpdate || string(c.Before) != "Written by hand.\n" {
		t.Fatalf("change %+v", c)
	}
	var m manifest.Manifest
	if _, err := apply(env, changes, &recorder{t: t, root: env.Home}, &m); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "Managed.\n" {
		t.Errorf("content %q", got)
	}
	saved := loadManifest(t, env)
	if _, ok := saved.Get(path, manifest.UnitFile, ""); !ok {
		t.Error("adopted file not recorded")
	}
}

func TestDiffRemovesUnitsDroppedFromLibrary(t *testing.T) {
	env := home(t)
	write(t, at(env, ".claude/skills/x/SKILL.md"), "skill\n")
	write(t, at(env, ".claude/skills/x/scripts/run.sh"), "echo run\n")
	owned := []plan.Unit{
		unit(env, adapter.ClaudeCode, ".claude/CLAUDE.md", manifest.UnitSection, "", "Use tabs.\n"),
		unit(env, adapter.ClaudeCode, ".claude/settings.json", manifest.UnitJSON, "/model", `"example-model"`),
		unit(env, "claude-code,codex", ".claude/settings.json", manifest.UnitJSONElement, "/permissions/allow@=Bash(ls)", `"Bash(ls)"`),
		unit(env, adapter.Codex, ".codex/config.toml", manifest.UnitTOMLTable, "mcp_servers.docs", "[mcp_servers.docs]\ncommand = \"docs-server\"\n"),
		unit(env, adapter.ClaudeCode, ".claude/skills/x/SKILL.md", manifest.UnitFile, "", "skill\n"),
		unit(env, adapter.ClaudeCode, ".claude/skills/x/scripts/run.sh", manifest.UnitFile, "", "echo run\n"),
	}
	kept := unit(env, adapter.GeminiCLI, ".gemini/settings.json", manifest.UnitJSON, "/theme", `"dark"`)
	var m manifest.Manifest
	for _, u := range append(slices.Clone(owned), kept) {
		m.Put(entryFor(t, u))
	}

	changes := diff(t, nil, m, []string{adapter.ClaudeCode, adapter.Codex}, plan.Options{})
	wantOps := map[string]plan.Op{
		at(env, ".claude/CLAUDE.md"):               plan.OpUpdate,
		at(env, ".claude/settings.json"):           plan.OpUpdate,
		at(env, ".codex/config.toml"):              plan.OpUpdate,
		at(env, ".claude/skills/x/SKILL.md"):       plan.OpDelete,
		at(env, ".claude/skills/x/scripts/run.sh"): plan.OpDelete,
	}
	if len(changes) != len(wantOps) {
		t.Fatalf("changes %+v", changes)
	}
	for path, op := range wantOps {
		if c := only(t, changes, path); c.Op != op || c.Blocked != "" {
			t.Errorf("%s: op %s blocked %q", path, c.Op, c.Blocked)
		}
	}
	if _, err := apply(env, changes, &recorder{t: t, root: env.Home}, &m); err != nil {
		t.Fatal(err)
	}

	if got := read(t, at(env, ".claude/CLAUDE.md")); got != "# Personal notes\n\nKeep answers short.\n\n" {
		t.Errorf("CLAUDE.md %q", got)
	}
	settings := []byte(read(t, at(env, ".claude/settings.json")))
	if _, found, _ := structedit.JSONGet(settings, "/model"); found {
		t.Error("/model still present")
	}
	if got, _, _ := structedit.JSONGet(settings, "/permissions/allow"); string(got) != "[]" {
		t.Errorf("allow list %s", got)
	}
	if got := read(t, at(env, ".codex/config.toml")); strings.Contains(got, "mcp_servers") || !strings.Contains(got, "trust_level") {
		t.Errorf("config.toml %q", got)
	}
	for _, gone := range []string{".claude/skills/x", ".claude/skills"} {
		if entry, _ := fsx.Inspect(at(env, gone)); entry.Exists {
			t.Errorf("%s still exists", gone)
		}
	}
	if entry, _ := fsx.Inspect(at(env, ".claude")); !entry.Exists {
		t.Error(".claude was removed")
	}
	saved := loadManifest(t, env)
	if want := []manifest.Entry{entryFor(t, kept)}; !reflect.DeepEqual(saved.Entries, want) {
		t.Errorf("manifest %+v", saved.Entries)
	}
}

func TestDiffRemovalWithoutKnownRootBlocked(t *testing.T) {
	env := home(t)
	owned := unit(env, adapter.ClaudeCode, ".claude/CLAUDE.md", manifest.UnitSection, "", "Use tabs.\n")
	rootless := entryFor(t, owned)
	rootless.Root = ""
	var m manifest.Manifest
	m.Put(rootless)
	changes := diff(t, nil, m, []string{adapter.ClaudeCode}, plan.Options{})
	if c := only(t, changes, owned.Path); c.Blocked != "no root known for this file" {
		t.Errorf("blocked %q", c.Blocked)
	}
}

func TestDiffBlocksUnsupportedTOMLLayout(t *testing.T) {
	env := home(t)
	write(t, at(env, ".codex/config.toml"), "mcp_servers.docs.command = \"docs-server\"\n")
	desired := []plan.Unit{unit(env, adapter.Codex, ".codex/config.toml", manifest.UnitTOMLTable, "mcp_servers.docs", "[mcp_servers.docs]\ncommand = \"other\"\n")}
	c := only(t, diff(t, desired, manifest.Manifest{}, []string{adapter.Codex}, plan.Options{}), at(env, ".codex/config.toml"))
	if !strings.Contains(c.Blocked, structedit.ErrUnsupportedLayout.Error()) {
		t.Errorf("blocked %q", c.Blocked)
	}
}

func TestDiffBlocksWholeFileMixedWithPartialUnits(t *testing.T) {
	env := home(t)
	desired := []plan.Unit{
		unit(env, adapter.ClaudeCode, "notes/new.md", manifest.UnitFile, "", "whole\n"),
		unit(env, adapter.Codex, "notes/new.md", manifest.UnitSection, "", "part\n"),
	}
	c := only(t, diff(t, desired, manifest.Manifest{}, []string{adapter.ClaudeCode, adapter.Codex}, plan.Options{}), at(env, "notes/new.md"))
	if c.Blocked != "shared file conflict: claude-code, codex" {
		t.Errorf("blocked %q", c.Blocked)
	}
}

func TestDiffRejectsInvalidUnits(t *testing.T) {
	env := home(t)
	outside := unit(env, adapter.Codex, "x.md", manifest.UnitFile, "", "x\n")
	outside.Path = filepath.Join(filepath.Dir(env.Home), "elsewhere.md")
	relative := unit(env, adapter.Codex, "x.md", manifest.UnitFile, "", "x\n")
	relative.Path = "x.md"
	cases := map[string]plan.Unit{
		"bad key":      unit(env, adapter.Codex, "x.json", manifest.UnitJSON, "model", `"x"`),
		"bad json":     unit(env, adapter.Codex, "x.json", manifest.UnitJSON, "/model", `{`),
		"outside root": outside,
		"relative":     relative,
		"no tool":      unit(env, "", "x.md", manifest.UnitFile, "", "x\n"),
	}
	for name, u := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := plan.Diff([]plan.Unit{u}, manifest.Manifest{}, nil, plan.Options{}); err == nil {
				t.Error("no error")
			}
		})
	}
}

func snapshotUnits(env paths.Env) []plan.Unit {
	secret := unit(env, adapter.ClaudeCode, ".claude/settings.json", manifest.UnitJSON, "/env/API_TOKEN", `"value"`)
	secret.Secret = true
	return []plan.Unit{
		unit(env, adapter.ClaudeCode, ".claude/CLAUDE.md", manifest.UnitSection, "", "Use spaces.\n"),
		unit(env, adapter.ClaudeCode, ".claude/settings.json", manifest.UnitJSON, "/model", `"other-model"`),
		unit(env, adapter.ClaudeCode, ".claude/settings.json", manifest.UnitJSONElement, "/permissions/allow@=Bash(pwd)", `"Bash(pwd)"`),
		secret,
		unit(env, adapter.Codex, ".codex/config.toml", manifest.UnitTOMLTable, "mcp_servers.docs", "[mcp_servers.docs]\ncommand = \"other\"\n"),
		unit(env, adapter.ClaudeCode, ".agents/skills/x/SKILL.md", manifest.UnitFile, "", "skill\n"),
		unit(env, adapter.Codex, ".agents/skills/x/SKILL.md", manifest.UnitFile, "", "skill\n"),
	}
}

func TestApplySnapshotsBeforeFirstWrite(t *testing.T) {
	tools := []string{adapter.ClaudeCode, adapter.Codex}

	t.Run("snapshot first", func(t *testing.T) {
		env := home(t)
		settings := at(env, ".claude/settings.json")
		changes := diff(t, snapshotUnits(env), manifest.Manifest{}, tools, plan.Options{SharedState: []string{settings}})
		before := testenv.TreeHash(t, env.Home)
		snap := &recorder{t: t, root: env.Home}
		var m manifest.Manifest
		result, err := apply(env, changes, snap, &m)
		if err != nil {
			t.Fatal(err)
		}
		if snap.calls != 1 || snap.reason != "change" || snap.treeAt != before {
			t.Errorf("calls %d reason %q tree unchanged %v", snap.calls, snap.reason, snap.treeAt == before)
		}
		want := []plan.SnapshotEntry{
			{Tool: "claude-code,codex", Path: at(env, ".agents/skills/x/SKILL.md"), Root: env.Home},
			{Tool: adapter.ClaudeCode, Path: at(env, ".claude/CLAUDE.md"), Root: env.Home},
			{Tool: adapter.ClaudeCode, Path: settings, Root: env.Home, Keys: []string{"/env/API_TOKEN", "/model", "/permissions/allow"}, RedactKeys: []string{"/env/API_TOKEN"}},
			{Tool: adapter.Codex, Path: at(env, ".codex/config.toml"), Root: env.Home},
		}
		if !reflect.DeepEqual(snap.entries, want) {
			t.Errorf("entries\n got %+v\nwant %+v", snap.entries, want)
		}
		if result.SnapshotID != "snap-1" || len(result.Written) != 4 {
			t.Errorf("result %+v", result)
		}
		saved := loadManifest(t, env)
		if entry, _ := saved.Get(settings, manifest.UnitJSON, "/env/API_TOKEN"); !entry.Secret {
			t.Error("secret flag not recorded")
		}
	})

	t.Run("whole file without shared state", func(t *testing.T) {
		env := home(t)
		changes := diff(t, snapshotUnits(env), manifest.Manifest{}, tools, plan.Options{})
		snap := &recorder{t: t, root: env.Home}
		var m manifest.Manifest
		if _, err := apply(env, changes, snap, &m); err != nil {
			t.Fatal(err)
		}
		for _, e := range snap.entries {
			if e.Path == at(env, ".claude/settings.json") && (e.Keys != nil || !reflect.DeepEqual(e.RedactKeys, []string{"/env/API_TOKEN"})) {
				t.Errorf("entry %+v", e)
			}
		}
	})

	t.Run("snapshot failure writes nothing", func(t *testing.T) {
		env := home(t)
		changes := diff(t, snapshotUnits(env), manifest.Manifest{}, tools, plan.Options{})
		before := testenv.TreeHash(t, env.Home)
		snap := &recorder{t: t, root: env.Home, err: errors.New("disk full")}
		var m manifest.Manifest
		if _, err := apply(env, changes, snap, &m); err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("error %v", err)
		}
		if got := testenv.TreeHash(t, env.Home); got != before {
			t.Error("home changed after failed snapshot")
		}
		if entry, _ := fsx.Inspect(env.ManifestPath()); entry.Exists || len(m.Entries) != 0 {
			t.Error("manifest changed after failed snapshot")
		}
	})
}

func TestApplyStopsOnErrorAndRecordsOnlyWritten(t *testing.T) {
	env := home(t)
	desired := []plan.Unit{
		unit(env, adapter.Codex, "notes/a.md", manifest.UnitFile, "", "a\n"),
		unit(env, adapter.Codex, "notes/b.md", manifest.UnitFile, "", "b\n"),
		unit(env, adapter.Codex, "notes/c/c.md", manifest.UnitFile, "", "c\n"),
	}
	changes := diff(t, desired, manifest.Manifest{}, []string{adapter.Codex}, plan.Options{})
	write(t, at(env, "notes/c"), "now a file\n")
	var m manifest.Manifest
	result, err := apply(env, changes, &recorder{t: t, root: env.Home}, &m)
	if err == nil || !strings.Contains(err.Error(), at(env, "notes/c/c.md")) {
		t.Fatalf("error %v", err)
	}
	if want := []string{at(env, "notes/a.md"), at(env, "notes/b.md")}; !reflect.DeepEqual(result.Written, want) {
		t.Errorf("written %v", result.Written)
	}
	if result.SnapshotID != "snap-1" {
		t.Errorf("snapshot id %q", result.SnapshotID)
	}
	saved := loadManifest(t, env)
	var recorded []string
	for _, e := range saved.Entries {
		recorded = append(recorded, e.Path)
	}
	if want := []string{at(env, "notes/a.md"), at(env, "notes/b.md")}; !reflect.DeepEqual(recorded, want) {
		t.Errorf("recorded %v", recorded)
	}
}

func TestApplyRefusesWithoutHeldLock(t *testing.T) {
	env := home(t)
	released, err := fsx.Hold(env.LockPath(), 0)
	if err != nil {
		t.Fatal(err)
	}
	released.Release()
	for name, held := range map[string]*fsx.Held{"nil": nil, "released": released} {
		t.Run(name, func(t *testing.T) {
			desired := []plan.Unit{unit(env, adapter.Codex, "notes/a.md", manifest.UnitFile, "", "a\n")}
			changes := diff(t, desired, manifest.Manifest{}, []string{adapter.Codex}, plan.Options{})
			before := testenv.TreeHash(t, env.Home)
			snap := &recorder{t: t, root: env.Home}
			var m manifest.Manifest
			_, err := plan.Applier{}.Apply(held, changes, snap, &m, env.ManifestPath())
			if !errors.Is(err, fsx.ErrNotHeld) {
				t.Fatalf("error %v", err)
			}
			assertUntouched(t, env, before, snap)
		})
	}
}

func TestApplyRefusesFileChangedSincePlan(t *testing.T) {
	env := home(t)
	path := at(env, ".claude/settings.json")
	desired := []plan.Unit{
		unit(env, adapter.ClaudeCode, ".claude/CLAUDE.md", manifest.UnitSection, "", "Use spaces.\n"),
		unit(env, adapter.ClaudeCode, ".claude/settings.json", manifest.UnitJSON, "/model", `"other-model"`),
	}
	changes := diff(t, desired, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{})
	write(t, path, "{\"model\": \"example-model\", \"written\": \"by the tool\"}\n")
	var m manifest.Manifest
	result, err := apply(env, changes, &recorder{t: t, root: env.Home}, &m)
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "changed since the plan") {
		t.Fatalf("error %v", err)
	}
	if got := read(t, path); !strings.Contains(got, "by the tool") {
		t.Errorf("tool write lost: %q", got)
	}
	if !reflect.DeepEqual(result.Written, []string{at(env, ".claude/CLAUDE.md")}) {
		t.Errorf("written %v", result.Written)
	}
	saved := loadManifest(t, env)
	if _, ok := saved.Get(path, manifest.UnitJSON, "/model"); ok {
		t.Error("unwritten unit recorded")
	}
}

func TestSharedEntryKeptForToolsOutsideSync(t *testing.T) {
	env := home(t)
	path := at(env, ".claude/settings.json")
	shared := unit(env, "claude-code,codex,gemini-cli", ".claude/settings.json", manifest.UnitJSONElement, "/permissions/allow@=Bash(ls)", `"Bash(ls)"`)
	var m manifest.Manifest
	m.Put(entryFor(t, shared))
	changes := diff(t, nil, m, []string{adapter.ClaudeCode, adapter.Codex}, plan.Options{})
	c := only(t, changes, path)
	if c.Op != plan.OpNoop || c.Blocked != "" || len(c.Removed) != 0 || len(c.Retagged) != 1 || c.Retagged[0].Tool != adapter.GeminiCLI {
		t.Fatalf("change %+v", c)
	}
	before := testenv.TreeHash(t, env.Home)
	snap := &recorder{t: t, root: env.Home}
	result, err := apply(env, changes, snap, &m)
	if err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, env, before, snap)
	if len(result.Written) != 0 {
		t.Errorf("written %v", result.Written)
	}
	saved := loadManifest(t, env)
	want := entryFor(t, shared)
	want.Tool = adapter.GeminiCLI
	if !reflect.DeepEqual(saved.Entries, []manifest.Entry{want}) {
		t.Errorf("manifest %+v", saved.Entries)
	}
}

func applied(t *testing.T, env paths.Env, m *manifest.Manifest, desired []plan.Unit) *recorder {
	t.Helper()
	changes := diff(t, desired, *m, []string{adapter.Cursor}, plan.Options{})
	for _, c := range changes {
		if c.Blocked != "" {
			t.Fatalf("%s blocked %q", c.Path, c.Blocked)
		}
	}
	snap := &recorder{t: t, root: env.Home}
	if _, err := apply(env, changes, snap, m); err != nil {
		t.Fatal(err)
	}
	return snap
}

func adoptedFlags(m manifest.Manifest) map[string]bool {
	flags := map[string]bool{}
	for _, e := range m.Entries {
		flags[filepath.Base(e.Path)+" "+e.Key] = e.Adopted
	}
	return flags
}

func TestAdoptedUnitReleasedNotDeleted(t *testing.T) {
	const document = `{"version": 1, "mine": true}`
	settings := func(env paths.Env) plan.Unit {
		return unit(env, adapter.Cursor, "tool/hooks.json", manifest.UnitJSON, "/version", "1")
	}
	file := func(env paths.Env) plan.Unit {
		return unit(env, adapter.Cursor, "tool/note.md", manifest.UnitFile, "", "mine\n")
	}
	for name, extra := range map[string]bool{"unchanged file": false, "file that also gets a write": true} {
		t.Run(name, func(t *testing.T) {
			env := home(t)
			write(t, at(env, "tool/hooks.json"), document)
			write(t, at(env, "tool/note.md"), "mine\n")
			desired := []plan.Unit{settings(env), file(env)}
			if extra {
				desired = append(desired, unit(env, adapter.Cursor, "tool/hooks.json", manifest.UnitJSON, "/ours", "2"))
			}
			var m manifest.Manifest
			applied(t, env, &m, desired)
			want := map[string]bool{"hooks.json /version": true, "note.md ": true}
			if extra {
				want["hooks.json /ours"] = false
			}
			if got := adoptedFlags(loadManifest(t, env)); !reflect.DeepEqual(got, want) {
				t.Fatalf("adopted %v", got)
			}

			for i := range desired {
				desired[i].Items = []library.ID{"item-renamed"}
			}
			applied(t, env, &m, desired)
			if got := adoptedFlags(loadManifest(t, env)); !reflect.DeepEqual(got, want) {
				t.Fatalf("adopted after a second apply %v", got)
			}

			changes := diff(t, nil, m, []string{adapter.Cursor}, plan.Options{})
			note := only(t, changes, at(env, "tool/note.md"))
			if note.Op != plan.OpNoop || note.Blocked != "" || len(note.Removed) != 1 {
				t.Errorf("note op %s blocked %q removed %d", note.Op, note.Blocked, len(note.Removed))
			}
			hooks := only(t, changes, at(env, "tool/hooks.json"))
			wantOp := plan.OpNoop
			if extra {
				wantOp = plan.OpUpdate
			}
			if hooks.Op != wantOp || hooks.Blocked != "" {
				t.Errorf("hooks op %s blocked %q", hooks.Op, hooks.Blocked)
			}
			applied(t, env, &m, nil)
			if got := read(t, at(env, "tool/note.md")); got != "mine\n" {
				t.Errorf("note %q", got)
			}
			after := []byte(read(t, at(env, "tool/hooks.json")))
			if _, found, _ := structedit.JSONGet(after, "/version"); !found {
				t.Errorf("version removed: %s", after)
			}
			if _, found, _ := structedit.JSONGet(after, "/ours"); found {
				t.Errorf("written key kept: %s", after)
			}
			if saved := loadManifest(t, env); len(saved.Entries) != 0 {
				t.Errorf("manifest %+v", saved.Entries)
			}
		})
	}
}

func TestAdoptedUnitReleasedThroughSymlink(t *testing.T) {
	env := home(t)
	write(t, at(env, "tool/note.md"), "mine\n")
	var m manifest.Manifest
	applied(t, env, &m, []plan.Unit{unit(env, adapter.Cursor, "tool/note.md", manifest.UnitFile, "", "mine\n")})
	write(t, at(env, "elsewhere.md"), "moved\n")
	link(t, at(env, "elsewhere.md"), at(env, "tool/note.md"))
	before := testenv.TreeHash(t, env.Home)
	snap := applied(t, env, &m, nil)
	assertUntouched(t, env, before, snap)
	if saved := loadManifest(t, env); len(saved.Entries) != 0 {
		t.Errorf("manifest %+v", saved.Entries)
	}
}

func TestWrittenUnitStillDeleted(t *testing.T) {
	env := home(t)
	write(t, at(env, "tool/hooks.json"), `{"changed": 1}`)
	var m manifest.Manifest
	applied(t, env, &m, []plan.Unit{
		unit(env, adapter.Cursor, "tool/hooks.json", manifest.UnitJSON, "/changed", "1"),
		unit(env, adapter.Cursor, "tool/hooks.json", manifest.UnitJSON, "/created", "true"),
		unit(env, adapter.Cursor, "tool/note.md", manifest.UnitFile, "", "ours\n"),
	})
	applied(t, env, &m, []plan.Unit{
		unit(env, adapter.Cursor, "tool/hooks.json", manifest.UnitJSON, "/changed", "2"),
		unit(env, adapter.Cursor, "tool/hooks.json", manifest.UnitJSON, "/created", "true"),
		unit(env, adapter.Cursor, "tool/note.md", manifest.UnitFile, "", "ours\n"),
	})
	want := map[string]bool{"hooks.json /changed": false, "hooks.json /created": false, "note.md ": false}
	if got := adoptedFlags(loadManifest(t, env)); !reflect.DeepEqual(got, want) {
		t.Fatalf("adopted %v", got)
	}
	applied(t, env, &m, nil)
	after := []byte(read(t, at(env, "tool/hooks.json")))
	for _, key := range []string{"/changed", "/created"} {
		if _, found, _ := structedit.JSONGet(after, key); found {
			t.Errorf("%s still present: %s", key, after)
		}
	}
	if entry, _ := fsx.Inspect(at(env, "tool/note.md")); entry.Exists {
		t.Error("written file kept")
	}
}

func TestDiffKeepsEntriesListedInKeep(t *testing.T) {
	env := home(t)
	write(t, at(env, ".claude/skills/x/SKILL.md"), "skill\n")
	held := unit(env, adapter.ClaudeCode, ".claude/skills/x/SKILL.md", manifest.UnitFile, "", "skill\n")
	dropped := unit(env, adapter.ClaudeCode, ".claude/settings.json", manifest.UnitJSON, "/model", `"example-model"`)
	var m manifest.Manifest
	m.Put(entryFor(t, held))
	m.Put(entryFor(t, dropped))

	changes := diff(t, nil, m, []string{adapter.ClaudeCode}, plan.Options{Keep: []manifest.Entry{entryFor(t, held)}})

	for _, c := range changes {
		if c.Path == held.Path {
			t.Errorf("kept entry planned: op %s removed %d", c.Op, len(c.Removed))
		}
	}
	if c := only(t, changes, dropped.Path); len(c.Removed) != 1 {
		t.Errorf("dropped entry not removed: %+v", c.Removed)
	}
}

func TestReconcileDropsEntriesNoLongerOnDisk(t *testing.T) {
	env := home(t)
	write(t, at(env, ".claude/skills/x/SKILL.md"), "skill\n")
	write(t, at(env, ".claude/settings.json"), `{"model": "edited"}`+"\n")
	matching := unit(env, adapter.ClaudeCode, ".claude/skills/x/SKILL.md", manifest.UnitFile, "", "skill\n")
	edited := unit(env, adapter.ClaudeCode, ".claude/settings.json", manifest.UnitJSON, "/model", `"example-model"`)
	missing := unit(env, adapter.ClaudeCode, ".claude/agents/gone.md", manifest.UnitFile, "", "gone\n")
	untouched := unit(env, adapter.Codex, ".codex/config.toml", manifest.UnitTOMLTable, "mcp_servers.docs", "[mcp_servers.docs]\ncommand = \"docs-server\"\n")
	var m manifest.Manifest
	for _, u := range []plan.Unit{matching, edited, missing, untouched} {
		m.Put(entryFor(t, u))
	}

	removed, err := plan.Reconcile(&m, []string{matching.Path, edited.Path, missing.Path})
	if err != nil {
		t.Fatal(err)
	}

	if want := []manifest.Entry{entryFor(t, matching), entryFor(t, untouched)}; !reflect.DeepEqual(m.Entries, want) {
		t.Errorf("kept %+v", m.Entries)
	}
	if len(removed) != 2 {
		t.Errorf("removed %+v", removed)
	}
}
