package cursor_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/antonL95/promptstor/internal/adapter"
	_ "github.com/antonL95/promptstor/internal/adapter/cursor"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	homeMarker    = "<home>"
	projectMarker = "<project>"

	editorReason    = "not enforced in the Cursor editor"
	userRulesReason = "Cursor user rules live in your Cursor account"
	cliAgentsReason = "the Cursor CLI ignores ~/.cursor/agents"
	leakReason      = "Cursor also reads CLAUDE.md"
)

func registered(t *testing.T) adapter.Adapter {
	t.Helper()
	a, ok := adapter.Get(adapter.Cursor)
	if !ok {
		t.Fatal("cursor is not registered")
	}
	return a
}

func target(t *testing.T, fixture string, scope adapter.Scope, selected ...adapter.ToolID) adapter.Target {
	t.Helper()
	return adapter.Target{
		Env:         testenv.Home(t, filepath.Join("testdata", fixture, "home")),
		Scope:       scope,
		ProjectRoot: testenv.Home(t, filepath.Join("testdata", fixture, "project")).Home,
		Selected:    selected,
	}
}

func emptyTarget(t *testing.T, scope adapter.Scope, selected ...adapter.ToolID) adapter.Target {
	t.Helper()
	if len(selected) == 0 {
		selected = []adapter.ToolID{adapter.Cursor}
	}
	return adapter.Target{Env: testenv.Home(t, t.TempDir()), Scope: scope, ProjectRoot: t.TempDir(), Selected: selected}
}

func relative(tg adapter.Target, path string) string {
	path = strings.ReplaceAll(path, tg.ProjectRoot, projectMarker)
	return strings.ReplaceAll(path, tg.Env.Home, homeMarker)
}

func importFrom(t *testing.T, tg adapter.Target) adapter.Imported {
	t.Helper()
	imported, err := registered(t).Import(tg)
	if err != nil {
		t.Fatal(err)
	}
	return imported
}

func export(t *testing.T, tg adapter.Target, items ...library.Item) ([]plan.Unit, []adapter.Unsupported) {
	t.Helper()
	units, unsupported, err := registered(t).Export(tg, items)
	if err != nil {
		t.Fatal(err)
	}
	return units, unsupported
}

func diff(t *testing.T, units []plan.Unit) []plan.FileChange {
	t.Helper()
	changes, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.Cursor}, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return changes
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

type itemView struct {
	Kind        library.Kind                `yaml:"kind"`
	Name        string                      `yaml:"name"`
	Order       int                         `yaml:"order,omitempty"`
	Source      string                      `yaml:"source"`
	Description string                      `yaml:"description,omitempty"`
	Activation  library.Activation          `yaml:"activation,omitempty"`
	Scope       []string                    `yaml:"scope,omitempty"`
	Targets     []string                    `yaml:"targets,omitempty"`
	Meta        map[string]any              `yaml:"meta,omitempty"`
	Files       []string                    `yaml:"files,omitempty"`
	Native      map[string]any              `yaml:"native,omitempty"`
	Portable    *library.PortablePermission `yaml:"portable,omitempty"`
	MCP         *library.MCPServer          `yaml:"mcp,omitempty"`
	Body        string                      `yaml:"body,omitempty"`
}

type reportView struct {
	Items      []itemView           `yaml:"items"`
	Foreign    []adapter.Foreign    `yaml:"foreign,omitempty"`
	Grants     []adapter.Grant      `yaml:"grants,omitempty"`
	Notes      []adapter.Note       `yaml:"notes,omitempty"`
	Unresolved []adapter.Unresolved `yaml:"unresolved,omitempty"`
}

func importYAML(t *testing.T, tg adapter.Target, imported adapter.Imported) []byte {
	t.Helper()
	view := reportView{Items: []itemView{}}
	for _, it := range imported.Items {
		var files []string
		for _, name := range slices.Sorted(maps.Keys(it.Files)) {
			files = append(files, fmt.Sprintf("%s %#o", name, it.Files[name].Mode))
		}
		view.Items = append(view.Items, itemView{
			Kind: it.Kind, Name: it.Name, Order: it.Order, Source: relative(tg, it.Source), Description: it.Description,
			Activation: it.Activation, Scope: it.Scope, Targets: it.Targets.Include, Meta: it.Meta, Files: files,
			Native: it.Native, Portable: it.Portable, MCP: it.MCP, Body: it.Body,
		})
	}
	for _, f := range imported.Foreign {
		f.Path = relative(tg, f.Path)
		view.Foreign = append(view.Foreign, f)
	}
	for _, g := range imported.Grants {
		g.Path = relative(tg, g.Path)
		view.Grants = append(view.Grants, g)
	}
	for _, n := range imported.Notes {
		n.Path, n.Message = relative(tg, n.Path), relative(tg, n.Message)
		view.Notes = append(view.Notes, n)
	}
	view.Unresolved = imported.Unresolved
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	encoder.CompactSeqIndent()
	if err := encoder.Encode(view); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func describe(tg adapter.Target, units []plan.Unit) []byte {
	var out strings.Builder
	for _, u := range units {
		var items []string
		for _, id := range u.Items {
			items = append(items, string(id))
		}
		fmt.Fprintf(&out, "== %s %s key=%q root=%s mode=%#o items=%s\n%s\n",
			u.Kind, relative(tg, u.Path), relative(tg, u.Key), relative(tg, u.Root), u.Mode, strings.Join(items, ","), relative(tg, string(u.Content)))
	}
	return []byte(out.String())
}

func find(t *testing.T, items []library.Item, kind library.Kind, name string) library.Item {
	t.Helper()
	for _, it := range items {
		if it.Kind == kind && it.Name == name {
			return it
		}
	}
	t.Fatalf("no %s %q", kind, name)
	return library.Item{}
}

func rule(id, name string, activation library.Activation, scope ...string) library.Item {
	return library.Item{ID: library.ID(id), Kind: library.KindRule, Name: name, Activation: activation, Scope: scope, Body: "## " + name + "\n\nText.\n"}
}

func only(it library.Item, tool adapter.ToolID) library.Item {
	it.Targets = library.Targets{Include: []string{tool}}
	return it
}

func guard(commands, paths []string) library.Item {
	return library.Item{ID: "ps_perm", Kind: library.KindPermission, Name: "Guard", Portable: &library.PortablePermission{DenyCommands: commands, DenyPaths: paths}}
}

func server(id, name string, mcp library.MCPServer) library.Item {
	return library.Item{ID: library.ID(id), Kind: library.KindMCP, Name: name, MCP: &mcp}
}

func local(command string, args ...string) library.MCPServer {
	return library.MCPServer{Transport: "stdio", Command: command, Args: args}
}

func hook(id, name string, native map[string]any) library.Item {
	return library.Item{ID: library.ID(id), Kind: library.KindHook, Name: name, Native: map[string]any{adapter.Cursor: native}}
}

func unsupportedFor(id, name, reason string, blocking bool) adapter.Unsupported {
	return adapter.Unsupported{ItemID: library.ID(id), Name: name, Tool: adapter.Cursor, Reason: reason, Blocking: blocking}
}

func libraryItems() []library.Item {
	releases := rule("ps_agent", "Releases", library.ActivationAgent)
	releases.Description = "Use when preparing a release."
	return []library.Item{
		rule("ps_always", "Reviews", library.ActivationAlways),
		rule("ps_go", "Go style", library.ActivationFiles, "**/*.go", "go.mod"),
		releases,
		{ID: "ps_skill", Kind: library.KindSkill, Name: "release-notes", Description: "Draft release notes.", Body: "Summarise the changes.\n", Files: map[string]library.File{"scripts/run.sh": {Data: []byte("#!/bin/sh\n"), Mode: 0o755}}},
		{ID: "ps_command", Kind: library.KindCommand, Name: "git:commit", Body: "Write a commit message.\n"},
		{ID: "ps_sub", Kind: library.KindSubagent, Name: "code-reviewer", Description: "Reviews diffs.", Meta: map[string]any{
			"cursor_model": "inherit", "cursor_readonly": true, "cursor_is_background": false,
		}, Body: "Be strict.\n"},
		guard([]string{"rm", "sudo *"}, []string{"**/.env"}),
		hook("ps_hook", "Session note", map[string]any{"event": "sessionStart", "command": "/opt/example/hooks/start.sh", "timeout": 10}),
		server("ps_mcp", "tracker", library.MCPServer{Transport: "stdio", Command: "example-tracker", Args: []string{"--verbose"}, Env: map[string]library.Value{"MODE": {Literal: "readonly"}}}),
		server("ps_remote", "remote", library.MCPServer{Transport: "http", URL: "https://mcp.example.com/mcp", Headers: map[string]library.Value{"X-Team": {Literal: "a"}}}),
	}
}

func contents(units []plan.Unit) []string {
	var got []string
	for _, u := range units {
		got = append(got, u.Key+" "+string(u.Content))
	}
	return got
}

func TestDetect(t *testing.T) {
	a := registered(t)
	if a.ID() != adapter.Cursor {
		t.Errorf("ID %q", a.ID())
	}
	env := testenv.Home(t, t.TempDir())
	if got := a.Detect(env); got.Installed || len(got.Paths) != 0 {
		t.Errorf("empty home %+v", got)
	}
	dir := filepath.Join(env.Home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := a.Detect(env); !got.Installed || !reflect.DeepEqual(got.Paths, []string{dir}) {
		t.Errorf("config dir %+v", got)
	}
	for _, binary := range []string{"cursor", "cursor-agent"} {
		env := testenv.Home(t, t.TempDir())
		env.LookPath = func(name string) (string, error) {
			if name == binary {
				return "/usr/local/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		}
		if got := a.Detect(env); !got.Installed {
			t.Errorf("%s on PATH %+v", binary, got)
		}
	}
}

func TestSelectableWhenNotInstalled(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeProject)
	if registered(t).Detect(tg.Env).Installed {
		t.Fatal("empty home counted as installed")
	}
	units, unsupported := export(t, tg, only(rule("ps_go", "Go style", library.ActivationFiles, "**/*.go"), adapter.Cursor), server("ps_mcp", "s", local("x")))
	if len(units) != 2 || len(unsupported) != 0 {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestCapabilities(t *testing.T) {
	caps := registered(t).Capabilities()
	var kinds, activations, homes []string
	for _, kind := range slices.Sorted(maps.Keys(caps.Kinds)) {
		if caps.Kinds[kind] {
			kinds = append(kinds, string(kind))
		}
	}
	for _, activation := range slices.Sorted(maps.Keys(caps.Activations)) {
		if caps.Activations[activation] {
			activations = append(activations, string(activation))
		}
	}
	for _, scope := range slices.Sorted(maps.Keys(caps.SkillHomes)) {
		homes = append(homes, string(scope)+"="+strings.Join(caps.SkillHomes[scope], ","))
	}
	ref := "none"
	if caps.EnvRef != nil {
		ref = caps.EnvRef("NAME")
	}
	table := fmt.Sprintf("kinds: %s\nfile scope: %v\nactivations: %s\nportable: deny-commands=%v deny-paths=%v allow-mcp-servers=%v\nreads project AGENTS.md: %v\nskill homes: %s\ncommands as skills: %v\nenv ref: %s\n",
		strings.Join(kinds, ","), caps.FileScope, strings.Join(activations, ","),
		caps.Portable.DenyCommands, caps.Portable.DenyPaths, caps.Portable.AllowMCPServers,
		caps.ReadsProjectAgentsMD, strings.Join(homes, " "), caps.CommandsAsSkills, ref)
	testenv.Golden(t, "testdata/golden/capabilities.txt", []byte(table))
}

func TestImportUserScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Cursor)
	before := testenv.TreeHash(t, tg.Env.Home)
	testenv.Golden(t, "testdata/golden/import-user.yaml", importYAML(t, tg, importFrom(t, tg)))
	if testenv.TreeHash(t, tg.Env.Home) != before {
		t.Error("import changed the home")
	}
}

func TestImportProjectScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.Cursor, adapter.AgentsMD)
	testenv.Golden(t, "testdata/golden/import-project.yaml", importYAML(t, tg, importFrom(t, tg)))
}

func TestSecretsScrubbedOnImport(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Cursor)
	imported := importFrom(t, tg)
	report := importYAML(t, tg, imported)
	for _, secret := range []string{"fixture-token-value", "fixture-bearer-value", "fixture-oauth-secret"} {
		if bytes.Contains(report, []byte(secret)) {
			t.Errorf("import carries %s", secret)
		}
	}
	tracker := find(t, imported.Items, library.KindMCP, "tracker")
	if tracker.MCP.Env["MODE"] != (library.Value{Literal: "readonly"}) || tracker.MCP.Env["TRACKER_TOKEN"].Literal != "" {
		t.Errorf("tracker %+v", tracker.MCP)
	}
}

func TestCursorVariablesNotImported(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	path := filepath.Join(tg.Env.Home, ".cursor", "mcp.json")
	write(t, path, `{"mcpServers":{"a":{"command":"x","args":["${userHome}/a"]},"b":{"command":"${workspaceFolderBasename}"},"c":{"url":"https://example.com","headers":{"X":"${pathSeparator}"}},"d":{"command":"x","env":{"P":"a${/}b"}}}}`)
	imported := importFrom(t, tg)
	if len(imported.Items) != 0 || len(imported.Notes) != 4 {
		t.Errorf("imported %s", importYAML(t, tg, imported))
	}
}

func TestCredentialLinksNeverRead(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	secret := filepath.Join(tg.Env.Home, ".codex", "auth.json")
	write(t, secret, `{"mcpServers":{"leak":{"command":"fixture-linked-secret"}}}`)
	if err := os.MkdirAll(filepath.Join(tg.Env.Home, ".cursor"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mcp.json", "hooks.json", "cli-config.json"} {
		if err := os.Symlink(secret, filepath.Join(tg.Env.Home, ".cursor", name)); err != nil {
			t.Fatal(err)
		}
	}
	imported := importFrom(t, tg)
	if len(imported.Items) != 0 || bytes.Contains(importYAML(t, tg, imported), []byte("fixture-linked-secret")) {
		t.Errorf("imported %s", importYAML(t, tg, imported))
	}
}

func TestExportUserScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Cursor)
	items := libraryItems()
	units, unsupported := export(t, tg, items...)
	testenv.Golden(t, "testdata/golden/units-user.txt", describe(tg, units))
	want := []adapter.Unsupported{
		unsupportedFor("ps_always", "Reviews", userRulesReason, false),
		unsupportedFor("ps_go", "Go style", userRulesReason, false),
		unsupportedFor("ps_agent", "Releases", userRulesReason, false),
		unsupportedFor("ps_sub", "code-reviewer", cliAgentsReason, false),
		unsupportedFor("ps_perm", "Guard", editorReason, true),
	}
	if !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}
	if !reflect.DeepEqual(items, libraryItems()) {
		t.Error("export changed the items it was given")
	}
	before := testenv.TreeHash(t, tg.Env.Home)
	blocked := map[string]string{}
	for _, change := range diff(t, units) {
		if change.Blocked != "" {
			blocked[relative(tg, change.Path)] = change.Blocked
		}
	}
	wantBlocked := map[string]string{
		"<home>/.agents/skills/release-notes/SKILL.md":       "not created by promptstor",
		"<home>/.agents/skills/release-notes/scripts/run.sh": "not created by promptstor",
		"<home>/.cursor/commands/git-commit.md":              "not created by promptstor",
	}
	if !maps.Equal(blocked, wantBlocked) {
		t.Errorf("blocked %v", blocked)
	}
	if testenv.TreeHash(t, tg.Env.Home) != before {
		t.Error("the home changed")
	}
}

func TestExportProjectScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.Cursor, adapter.ClaudeCode, adapter.AgentsMD)
	tg.ProjectRoot = t.TempDir()
	mine := only(rule("ps_mine", "Cursor notes", ""), adapter.Cursor)
	theirs := only(rule("ps_claude", "Claude notes", ""), adapter.ClaudeCode)
	items := append(libraryItems(), mine, theirs)
	units, unsupported := export(t, tg, items...)
	testenv.Golden(t, "testdata/golden/units-project.txt", describe(tg, units))
	want := []adapter.Unsupported{
		unsupportedFor("ps_claude", "Claude notes", leakReason, false),
		unsupportedFor("ps_perm", "Guard", editorReason, true),
	}
	if !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}
	for _, change := range diff(t, units) {
		if change.Op != plan.OpCreate || change.Blocked != "" {
			t.Errorf("%s %s %q", change.Op, relative(tg, change.Path), change.Blocked)
		}
		if strings.HasSuffix(change.Path, ".json") && !json.Valid(change.After) {
			t.Errorf("%s is not JSON\n%s", relative(tg, change.Path), change.After)
		}
	}
}

func TestClaudeRulesNotReportedWithoutClaude(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeProject, adapter.Cursor, adapter.Codex)
	units, unsupported := export(t, tg, only(rule("ps_claude", "Claude notes", ""), adapter.ClaudeCode))
	if len(units) != 0 || len(unsupported) != 0 {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestUserRulesUnsupported(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	units, unsupported := export(t, tg, rule("ps_a", "One", ""), rule("ps_b", "Two", library.ActivationFiles, "*.go"))
	want := []adapter.Unsupported{
		unsupportedFor("ps_a", "One", userRulesReason, false),
		unsupportedFor("ps_b", "Two", userRulesReason, false),
	}
	if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestMDCActivationMapping(t *testing.T) {
	described := func(it library.Item, text string) library.Item {
		it.Description = text
		return it
	}
	cases := []struct {
		item library.Item
		want string
	}{
		{rule("a", "One", ""), "---\nalwaysApply: true\n---\n"},
		{described(rule("b", "Two", library.ActivationAlways), "Kept."), "---\ndescription: Kept.\nalwaysApply: true\n---\n"},
		{rule("c", "Three", library.ActivationFiles, "*.ts"), "---\nglobs: *.ts\nalwaysApply: false\n---\n"},
		{rule("d", "Four", "", "web/**"), "---\nglobs: web/**\nalwaysApply: false\n---\n"},
		{described(rule("e", "Five", library.ActivationAgent), "When: releasing"), "---\ndescription: 'When: releasing'\nalwaysApply: false\n---\n"},
		{described(rule("f", "Six", library.ActivationAgent), strings.Repeat("long words ", 20)+"end"), "---\ndescription: " + strings.Repeat("long words ", 20) + "end\nalwaysApply: false\n---\n"},
	}
	for _, c := range cases {
		tg := emptyTarget(t, adapter.ScopeProject, adapter.Cursor, adapter.Codex)
		units, unsupported := export(t, tg, only(c.item, adapter.Cursor))
		if len(units) != 1 || len(unsupported) != 0 {
			t.Fatalf("%s units %s unsupported %+v", c.item.Name, describe(tg, units), unsupported)
		}
		path := filepath.Join(tg.ProjectRoot, ".cursor", "rules", strings.ToLower(c.item.Name)+".mdc")
		if units[0].Path != path || units[0].Kind != manifest.UnitFile {
			t.Errorf("%s unit %s", c.item.Name, describe(tg, units))
		}
		want := c.want + managed.Notice + "\n" + c.item.Body
		if got := string(units[0].Content); got != want {
			t.Errorf("%s content %q want %q", c.item.Name, got, want)
		}
	}
}

func TestGlobsCommaString(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeProject)
	item := only(rule("a", "Go", library.ActivationFiles, "**/*.go", "go.mod", "cmd/**"), adapter.Cursor)
	units, _ := export(t, tg, item)
	if len(units) != 1 || !bytes.Contains(units[0].Content, []byte("\nglobs: **/*.go, go.mod, cmd/**\n")) {
		t.Fatalf("units %s", describe(tg, units))
	}
	write(t, units[0].Path, string(units[0].Content))
	imported := importFrom(t, tg)
	got := find(t, imported.Items, library.KindRule, "Go")
	if got.Activation != library.ActivationFiles || !reflect.DeepEqual(got.Scope, item.Scope) || got.Body != item.Body {
		t.Errorf("imported %+v", got)
	}
}

func TestInexpressibleRulesReported(t *testing.T) {
	cases := []struct {
		item   library.Item
		reason string
	}{
		{rule("a", "No globs", library.ActivationFiles), "Cursor needs globs for a file rule"},
		{rule("b", "No summary", library.ActivationAgent), "Cursor needs a description for an agent rule"},
		{rule("c", "Braces", library.ActivationFiles, "*.{ts,tsx}"), `glob "*.{ts,tsx}" cannot be written in Cursor's comma separated globs`},
		{rule("d", "Comment", library.ActivationFiles, "a #b"), `glob "a #b" cannot be written in Cursor's comma separated globs`},
	}
	for _, c := range cases {
		tg := emptyTarget(t, adapter.ScopeProject)
		units, unsupported := export(t, tg, c.item)
		want := []adapter.Unsupported{unsupportedFor(string(c.item.ID), c.item.Name, c.reason, false)}
		if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
			t.Errorf("%s units %s unsupported %+v", c.item.Name, describe(tg, units), unsupported)
		}
	}
	tg := emptyTarget(t, adapter.ScopeProject, adapter.Cursor, adapter.Codex)
	units, unsupported := export(t, tg, only(rule("e", "!!", ""), adapter.Cursor))
	want := []adapter.Unsupported{unsupportedFor("e", "!!", "the name has no characters usable in a file name", false)}
	if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestSameRuleSlugKeepsBothFiles(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeProject)
	units, _ := export(t, tg, rule("a", "Notes", library.ActivationFiles, "a"), rule("b", "notes", library.ActivationFiles, "b"))
	var names []string
	for _, u := range units {
		names = append(names, filepath.Base(u.Path))
	}
	if !reflect.DeepEqual(names, []string{"notes-2.mdc", "notes.mdc"}) {
		t.Errorf("names %v", names)
	}
}

func TestCommandsWriteNoFrontmatter(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := emptyTarget(t, scope)
		command := library.Item{ID: "ps_cmd", Kind: library.KindCommand, Name: "git:Commit All", Description: "Commit.", Meta: map[string]any{"argument-hint": "[scope]"}, Body: "Commit.\n"}
		units, unsupported := export(t, tg, command)
		want := []adapter.Unsupported{unsupportedFor("ps_cmd", "git:Commit All", "written without argument-hint, description, because Cursor command frontmatter is not documented", false)}
		if len(units) != 1 || !reflect.DeepEqual(unsupported, want) {
			t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
		}
		root := tg.Env.Home
		if scope == adapter.ScopeProject {
			root = tg.ProjectRoot
		}
		if units[0].Path != filepath.Join(root, ".cursor", "commands", "git-commit-all.md") || string(units[0].Content) != managed.Notice+"\nCommit.\n" {
			t.Errorf("%s units %s", scope, describe(tg, units))
		}
	}
}

func TestSubagentFrontmatter(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeProject)
	item := library.Item{ID: "ps_sub", Kind: library.KindSubagent, Name: "Code Reader", Description: "Reads.", Meta: map[string]any{
		"tools": "Read", "model": "sonnet", "cursor_model": "example-model[effort=high]", "cursor_readonly": "yes", "cursor_is_background": true,
	}, Body: "Read.\n"}
	units, unsupported := export(t, tg, item)
	want := []adapter.Unsupported{
		unsupportedFor("ps_sub", "Code Reader", "Cursor subagents have no tools field, so tools does not restrict this one", false),
		unsupportedFor("ps_sub", "Code Reader", "written without cursor_readonly, model, which Cursor agent files do not take", false),
	}
	if len(units) != 1 || !reflect.DeepEqual(unsupported, want) {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	content := "---\nname: code-reader\ndescription: Reads.\nmodel: example-model[effort=high]\nis_background: true\n---\n" + managed.Notice + "\nRead.\n"
	if got := string(units[0].Content); got != content || units[0].Path != filepath.Join(tg.ProjectRoot, ".cursor", "agents", "code-reader.md") {
		t.Errorf("content %q", got)
	}
}

func TestSkillsWrittenToSharedHome(t *testing.T) {
	skill := library.Item{ID: "ps_skill", Kind: library.KindSkill, Name: "release-notes", Description: "Draft.", Body: "x\n"}
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := emptyTarget(t, scope)
		units, unsupported := export(t, tg, skill)
		shared, err := adapter.SkillUnits(tg, adapter.Cursor, skill)
		if err != nil {
			t.Fatal(err)
		}
		if len(unsupported) != 0 || !reflect.DeepEqual(units, shared) || !strings.Contains(units[0].Path, filepath.Join(".agents", "skills")) {
			t.Errorf("%s units %s unsupported %+v", scope, describe(tg, units), unsupported)
		}
	}
}

func TestEditorDenyAckRequired(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := emptyTarget(t, scope)
		units, unsupported := export(t, tg, guard([]string{"rm -f *", "sudo"}, []string{"secrets/**"}))
		want := []adapter.Unsupported{
			unsupportedFor("ps_perm", "Guard", `deny command "rm -f *" cannot be written; Cursor Shell rules match the first word of a command`, true),
			unsupportedFor("ps_perm", "Guard", editorReason, true),
		}
		if !reflect.DeepEqual(unsupported, want) {
			t.Errorf("%s unsupported %+v", scope, unsupported)
		}
		file := filepath.Join(tg.Env.Home, ".cursor", "cli-config.json")
		if scope == adapter.ScopeProject {
			file = filepath.Join(tg.ProjectRoot, ".cursor", "cli.json")
		}
		var rules []string
		for _, u := range units {
			var value string
			if err := json.Unmarshal(u.Content, &value); err != nil {
				t.Fatal(err)
			}
			if u.Path != file || u.Kind != manifest.UnitJSONElement || u.Key != "/permissions/deny@="+value {
				t.Errorf("%s unit %s", scope, describe(tg, units))
			}
			rules = append(rules, value)
		}
		if want := []string{"Read(secrets/**)", "Shell(sudo)", "Write(secrets/**)"}; !reflect.DeepEqual(rules, want) {
			t.Errorf("%s rules %v", scope, rules)
		}
	}
}

func TestCLIConfigOnlyPermissionsOwned(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Cursor)
	units, _ := export(t, tg, guard([]string{"rm"}, nil))
	for _, u := range units {
		if u.Kind != manifest.UnitJSONElement || !strings.HasPrefix(u.Key, "/permissions/deny@=") {
			t.Errorf("unit %s", describe(tg, units))
		}
	}
	changes := diff(t, units)
	if len(changes) != 1 || changes[0].Blocked != "" {
		t.Fatalf("changes %+v", changes)
	}
	before, after := string(changes[0].Before), string(changes[0].After)
	for _, kept := range []string{`"version": 1`, `"vimMode": false`, `"modelId": "example-model"`, `"hasChangedDefaultModel": true`, `"allow": ["Shell(git)", "Read(src/**)"]`, `"Shell(curl:*)"`, `"Shell(rm)"`} {
		if !strings.Contains(after, kept) {
			t.Errorf("missing %s in\n%s", kept, after)
		}
	}
	start := strings.Index(before, `"deny"`)
	if !strings.HasPrefix(after, before[:start]) || !json.Valid(changes[0].After) {
		t.Errorf("bytes before the deny list changed\n%s", after)
	}
}

func TestInexpressibleDeniesBlocked(t *testing.T) {
	check := func(scope adapter.Scope, label string, item library.Item) {
		tg := emptyTarget(t, scope)
		units, unsupported := export(t, tg, item)
		if len(units) != 0 || len(unsupported) != 1 || !unsupported[0].Blocking {
			t.Errorf("%q units %s unsupported %+v", label, describe(tg, units), unsupported)
		}
	}
	for _, command := range []string{"", "  ", "*", "git push", "curl:*", "npm*", "a(b)", `"rm"`, "a&&b", "a|b", "a;b", "a\nb", "$(id)"} {
		check(adapter.ScopeUser, command, guard([]string{command}, nil))
	}
	for _, path := range []string{"", "!keep", "a{b,c}", "a[0]", "a(1)", "~other/x", "a\nb", `a\b`} {
		check(adapter.ScopeUser, path, guard(nil, []string{path}))
	}
	check(adapter.ScopeProject, "home path in a project", guard(nil, []string{"~/.ssh/**"}))
}

func TestHomeDenyPathsStayPortable(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	units, _ := export(t, tg, guard(nil, []string{"~/.ssh/**"}))
	want := []string{
		"/permissions/deny@=Read(" + tg.Env.Home + `/.ssh/**) "Read(` + tg.Env.Home + `/.ssh/**)"`,
		"/permissions/deny@=Write(" + tg.Env.Home + `/.ssh/**) "Write(` + tg.Env.Home + `/.ssh/**)"`,
	}
	if got := contents(units); !reflect.DeepEqual(got, want) {
		t.Fatalf("units %v", got)
	}
	for _, change := range diff(t, units) {
		write(t, change.Path, string(change.After))
	}
	permission := find(t, importFrom(t, tg).Items, library.KindPermission, "Cursor permissions")
	if !reflect.DeepEqual(permission.Portable.DenyPaths, []string{"~/.ssh/**"}) {
		t.Errorf("deny paths %v", permission.Portable.DenyPaths)
	}
}

func TestMovedCLIConfigBlocksDenies(t *testing.T) {
	override := emptyTarget(t, adapter.ScopeUser)
	override.Env.Getenv = func(name string) string {
		if name == "CURSOR_CONFIG_DIR" {
			return "/srv/example/cursor"
		}
		return ""
	}
	xdg := emptyTarget(t, adapter.ScopeUser)
	xdg.Env.OS, xdg.Env.ConfigHome = "linux", filepath.Join(xdg.Env.Home, "settings")
	present := emptyTarget(t, adapter.ScopeUser)
	present.Env.OS = "linux"
	write(t, filepath.Join(present.Env.ConfigHome, "cursor", "cli-config.json"), `{"permissions":{"deny":["Shell(x)"]}}`)
	reason := "the Cursor CLI may read cli-config.json from another folder here; denies not written"
	for label, tg := range map[string]adapter.Target{"override": override, "xdg": xdg, "present": present} {
		write(t, filepath.Join(tg.Env.Home, ".cursor", "cli-config.json"), `{"permissions":{"deny":["Shell(y)"]}}`)
		units, unsupported := export(t, tg, guard([]string{"rm"}, nil))
		if want := []adapter.Unsupported{unsupportedFor("ps_perm", "Guard", reason, true)}; len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
			t.Errorf("%s units %s unsupported %+v", label, describe(tg, units), unsupported)
		}
		if imported := importFrom(t, tg); len(imported.Items) != 0 || len(imported.Notes) != 1 {
			t.Errorf("%s imported %s", label, importYAML(t, tg, imported))
		}
	}
	project := override
	project.Scope = adapter.ScopeProject
	if units, _ := export(t, project, guard([]string{"rm"}, nil)); len(units) != 1 {
		t.Errorf("project units %s", describe(project, units))
	}
}

func TestHooksKeyedByCommand(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Cursor)
	audit := hook("ps_audit", "Audit", map[string]any{"event": "beforeShellExecution", "command": "./hooks/audit.sh", "timeout": 60})
	other := hook("ps_other", "Other", map[string]any{"event": "beforeShellExecution", "command": "./hooks/other.sh"})
	units, unsupported := export(t, tg, audit, other)
	want := []string{
		`/version 1`,
		`/hooks/beforeShellExecution@/command=./hooks/audit.sh {"command":"./hooks/audit.sh","timeout":60}`,
		`/hooks/beforeShellExecution@/command=./hooks/other.sh {"command":"./hooks/other.sh"}`,
	}
	if got := contents(units); len(unsupported) != 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("units %v unsupported %+v", got, unsupported)
	}
	if !reflect.DeepEqual(units[0].Items, []library.ID{"ps_audit", "ps_other"}) || units[0].Kind != manifest.UnitJSON {
		t.Errorf("version unit %s", describe(tg, units))
	}
	changes := diff(t, units)
	if len(changes) != 1 || changes[0].Blocked != "" {
		t.Fatalf("changes %+v", changes)
	}
	var doc struct {
		Version int
		Hooks   map[string][]map[string]any
	}
	if err := json.Unmarshal(changes[0].After, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || len(doc.Hooks["beforeShellExecution"]) != 2 || len(doc.Hooks["stop"]) != 1 {
		t.Errorf("document %s", changes[0].After)
	}
}

func TestInvalidHooksReported(t *testing.T) {
	for label, native := range map[string]map[string]any{
		"no event":      {"command": "x"},
		"unknown event": {"event": "PreToolUse", "command": "x"},
		"no command":    {"event": "stop"},
		"prompt":        {"event": "stop", "type": "prompt", "command": "x"},
		"unknown field": {"event": "stop", "command": "x", "async": true},
	} {
		tg := emptyTarget(t, adapter.ScopeUser)
		units, unsupported := export(t, tg, hook("ps_hook", "Bad", native))
		if len(units) != 0 || len(unsupported) != 1 || unsupported[0].Blocking {
			t.Errorf("%s units %s unsupported %+v", label, describe(tg, units), unsupported)
		}
	}
	tg := emptyTarget(t, adapter.ScopeUser)
	one := hook("ps_a", "A", map[string]any{"event": "stop", "command": "x"})
	two := hook("ps_b", "B", map[string]any{"event": "stop", "command": "x", "timeout": 5})
	units, unsupported := export(t, tg, one, two)
	want := []adapter.Unsupported{unsupportedFor("ps_b", "B", "another hook already uses this command for stop", false)}
	if len(units) != 2 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestOtherHooksVersionNotOverwritten(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	write(t, filepath.Join(tg.Env.Home, ".cursor", "hooks.json"), `{"version":2,"hooks":{}}`)
	units, unsupported := export(t, tg, hook("ps_hook", "Stop", map[string]any{"event": "stop", "command": "x"}))
	want := []adapter.Unsupported{unsupportedFor("ps_hook", "Stop", "hooks.json is not schema version 1; not written", false)}
	if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	if imported := importFrom(t, tg); len(imported.Items) != 0 || len(imported.Notes) != 1 {
		t.Errorf("imported %s", importYAML(t, tg, imported))
	}
}

func TestServerSettingsKept(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Cursor)
	units, unsupported := export(t, tg,
		server("ps_tracker", "tracker", local("new")),
		server("ps_local", "local", local("new")),
		server("ps_signed", "signed", library.MCPServer{Transport: "http", URL: "https://signed.example.com/mcp"}),
	)
	want := []string{
		`/mcpServers/local {"command":"new"}`,
		`/mcpServers/tracker {"type":"stdio","command":"new","envFile":".env.tracker"}`,
	}
	if got := contents(units); !reflect.DeepEqual(got, want) {
		t.Errorf("units %v", got)
	}
	dropped := []adapter.Unsupported{unsupportedFor("ps_signed", "signed", "mcpServers/signed has auth, which PromptStor would drop; set the server up by hand", false)}
	if !reflect.DeepEqual(unsupported, dropped) {
		t.Errorf("unsupported %+v", unsupported)
	}
}

func TestNativeEnvRefOnExport(t *testing.T) {
	if got := registered(t).Capabilities().EnvRef("NAME"); got != "${env:NAME}" {
		t.Errorf("env ref %q", got)
	}
	tg := emptyTarget(t, adapter.ScopeUser)
	units, unsupported := export(t, tg,
		server("ps_local", "a", library.MCPServer{Transport: "stdio", Command: "x", Args: []string{"--key=${env:API_KEY}"}, Env: map[string]library.Value{"API_KEY": {Ref: "env:API_KEY"}}}),
		server("ps_remote", "b/c", library.MCPServer{Transport: "sse", URL: "https://example.com/?r=${env:REGION}", Headers: map[string]library.Value{"Authorization": {Literal: "Bearer ${env:TOKEN}"}}}),
		server("ps_setup", "c", library.MCPServer{Transport: "stdio", Command: "x", Env: map[string]library.Value{"API_KEY": {Ref: "keychain:example/account"}}}),
	)
	want := []string{
		`/mcpServers/a {"type":"stdio","command":"x","args":["--key=${env:API_KEY}"],"env":{"API_KEY":"${env:API_KEY}"}}`,
		`/mcpServers/b~1c {"url":"https://example.com/?r=${env:REGION}","headers":{"Authorization":"Bearer ${env:TOKEN}"}}`,
	}
	if got := contents(units); !reflect.DeepEqual(got, want) {
		t.Errorf("units %v", got)
	}
	setup := []adapter.Unsupported{unsupportedFor("ps_setup", "c", "secret env/API_KEY needs setup before Cursor can use it", false)}
	if !reflect.DeepEqual(unsupported, setup) {
		t.Errorf("unsupported %+v", unsupported)
	}
}

func TestUnsupportedReported(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	items := []library.Item{
		rule("ps_rule", "Reviews", ""),
		{ID: "ps_skill", Kind: library.KindSkill, Name: "../escape", Description: "x", Body: "x\n"},
		{ID: "ps_skillname", Kind: library.KindSkill, Name: "Release Notes", Description: "x", Body: "x\n"},
		{ID: "ps_nodesc", Kind: library.KindSkill, Name: "quiet", Body: "x\n"},
		{ID: "ps_cred", Kind: library.KindSkill, Name: "keys", Description: "x", Body: "x\n", Files: map[string]library.File{"scripts/token.json": {Data: []byte("{}")}}},
		{ID: "ps_cmd", Kind: library.KindCommand, Name: "mute", Body: "  "},
		{ID: "ps_cmdname", Kind: library.KindCommand, Name: "!!", Body: "x\n"},
		{ID: "ps_cmdtext", Kind: library.KindCommand, Name: "binary", Body: "\xff"},
		{ID: "ps_subname", Kind: library.KindSubagent, Name: "??", Body: "x\n"},
		{ID: "ps_allow", Kind: library.KindPermission, Name: "Servers", Portable: &library.PortablePermission{AllowMCPServers: []string{"docs"}}},
		{ID: "ps_native", Kind: library.KindPermission, Name: "Native", Native: map[string]any{adapter.Cursor: map[string]any{}}},
		{ID: "ps_foreign", Kind: library.KindPermission, Name: "Claude only", Native: map[string]any{adapter.ClaudeCode: map[string]any{}}},
		{ID: "ps_hook", Kind: library.KindHook, Name: "Other tool", Native: map[string]any{adapter.ClaudeCode: map[string]any{}}},
		{ID: "ps_nomcp", Kind: library.KindMCP, Name: "empty"},
		server("ps_ws", "ws", library.MCPServer{Transport: "ws", URL: "wss://example.com"}),
		server("ps_both", "both", library.MCPServer{Transport: "http", URL: "https://example.com", Command: "x"}),
		server("ps_bare", "bare", library.MCPServer{Transport: "stdio"}),
	}
	units, unsupported := export(t, tg, items...)
	if len(units) != 0 {
		t.Errorf("units %s", describe(tg, units))
	}
	var got []string
	for _, u := range unsupported {
		if u.Tool != adapter.Cursor || u.Blocking {
			t.Errorf("unsupported %+v", u)
		}
		got = append(got, string(u.ItemID)+": "+u.Reason)
	}
	testenv.Golden(t, "testdata/golden/unsupported.txt", []byte(strings.Join(got, "\n")+"\n"))
}

func TestBackupSet(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.Cursor)
		var out strings.Builder
		for _, e := range registered(t).BackupSet(tg) {
			fmt.Fprintf(&out, "%s root=%s keys=%v redact=%v\n", relative(tg, e.Path), relative(tg, e.Root), e.Keys, e.RedactKeys)
		}
		testenv.Golden(t, "testdata/golden/backup-"+string(scope)+".txt", []byte(out.String()))
	}
}

func TestRoundTripNoChanges(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			tg := target(t, "roundtrip", scope, adapter.Cursor, adapter.AgentsMD)
			homeBefore, projectBefore := testenv.TreeHash(t, tg.Env.Home), testenv.TreeHash(t, tg.ProjectRoot)
			imported := importFrom(t, tg)
			if len(imported.Items) == 0 || len(imported.Unresolved) != 0 {
				t.Fatalf("imported %s", importYAML(t, tg, imported))
			}
			for _, note := range imported.Notes {
				if !strings.HasSuffix(note.Message, "field envFile not imported") {
					t.Errorf("note %+v", note)
				}
			}
			units, unsupported := export(t, tg, imported.Items...)
			for _, u := range unsupported {
				if u.Reason != editorReason && u.Reason != cliAgentsReason {
					t.Errorf("unsupported %+v", u)
				}
			}
			for _, change := range diff(t, units) {
				if change.Op != plan.OpNoop || change.Blocked != "" {
					t.Errorf("%s %s %q\n%s", change.Op, relative(tg, change.Path), change.Blocked, change.After)
				}
			}
			wantUnits := map[adapter.Scope]int{adapter.ScopeUser: 10, adapter.ScopeProject: 9}
			if len(units) != wantUnits[scope] {
				t.Errorf("units %d\n%s", len(units), describe(tg, units))
			}
			if testenv.TreeHash(t, tg.Env.Home) != homeBefore || testenv.TreeHash(t, tg.ProjectRoot) != projectBefore {
				t.Error("the fixture changed")
			}
		})
	}
}

func TestNothingDroppedSilentlyOnImport(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	cursorDir := filepath.Join(tg.Env.Home, ".cursor")
	shared := filepath.Join(tg.Env.Home, ".agents", "skills")
	write(t, filepath.Join(cursorDir, "hooks.json"), `{"version":1,"hooks":{"stop":[{"type":"prompt","prompt":"Done?"}]}}`)
	write(t, filepath.Join(cursorDir, "commands", "ship.md"), "---\nname: release\n---\nShip.\n")
	write(t, filepath.Join(shared, "web", "forms", "SKILL.md"), "---\nname: forms\ndescription: x\n---\nx\n")
	write(t, filepath.Join(cursorDir, "skills", "web", "SKILL.md"), "---\nname: web\ndescription: Real.\n---\nx\n")
	imported := importFrom(t, tg)
	want := []adapter.Note{
		{Path: filepath.Join(shared, "web"), Message: "no SKILL.md here; skills in category folders are not imported"},
		{Path: filepath.Join(cursorDir, "commands", "ship.md"), Message: "field name not imported"},
		{Path: filepath.Join(cursorDir, "hooks.json"), Message: "hook in stop: PromptStor writes command hooks only; not imported"},
	}
	if !reflect.DeepEqual(imported.Notes, want) {
		t.Errorf("notes %+v", imported.Notes)
	}
	find(t, imported.Items, library.KindSkill, "web")
}

type noSnapshot struct{}

func (noSnapshot) Snapshot(string, []plan.SnapshotEntry) (string, error) { return "snap", nil }

func TestRemovingLastHookKeepsUserVersion(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	path := filepath.Join(tg.Env.Home, ".cursor", "hooks.json")
	write(t, path, `{"version":1,"hooks":{"stop":[{"command":"mine"}]}}`)
	var m manifest.Manifest
	apply := func(items ...library.Item) {
		units, _ := export(t, tg, items...)
		changes, err := plan.Diff(units, m, []string{adapter.Cursor}, plan.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := plan.Apply(changes, noSnapshot{}, &m, tg.Env.ManifestPath(), tg.Env.LockPath()); err != nil {
			t.Fatal(err)
		}
	}
	apply(hook("ps_hook", "Ours", map[string]any{"event": "stop", "command": "ours"}))
	apply()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version int
		Hooks   map[string][]map[string]any
	}
	if err := json.Unmarshal(after, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || len(doc.Hooks["stop"]) != 1 || doc.Hooks["stop"][0]["command"] != "mine" || len(m.Entries) != 0 {
		t.Errorf("document %s manifest %+v", after, m.Entries)
	}
}
