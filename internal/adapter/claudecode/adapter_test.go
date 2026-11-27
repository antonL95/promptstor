package claudecode_test

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/adapter/claudecode"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/secrets"
	"github.com/antonL95/promptstor/internal/structedit"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	homeMarker    = "<home>"
	projectMarker = "<project>"
)

func registered(t *testing.T) adapter.Adapter {
	t.Helper()
	a, ok := adapter.Get(adapter.ClaudeCode)
	if !ok {
		t.Fatal("claude-code is not registered")
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

func relative(tg adapter.Target, path string) string {
	path = strings.Replace(path, tg.ProjectRoot, projectMarker, 1)
	return strings.Replace(path, tg.Env.Home, homeMarker, 1)
}

func importFrom(t *testing.T, tg adapter.Target) adapter.Imported {
	t.Helper()
	imported, err := registered(t).Import(tg)
	if err != nil {
		t.Fatal(err)
	}
	return imported
}

func export(t *testing.T, tg adapter.Target, items []library.Item) ([]plan.Unit, []adapter.Unsupported) {
	t.Helper()
	units, unsupported, err := registered(t).Export(tg, items)
	if err != nil {
		t.Fatal(err)
	}
	return units, unsupported
}

type itemView struct {
	Kind        library.Kind                `yaml:"kind"`
	Name        string                      `yaml:"name"`
	Order       int                         `yaml:"order,omitempty"`
	Source      string                      `yaml:"source"`
	Description string                      `yaml:"description,omitempty"`
	Targets     []string                    `yaml:"targets,omitempty"`
	Activation  library.Activation          `yaml:"activation,omitempty"`
	Scope       []string                    `yaml:"scope,omitempty"`
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
			Kind: it.Kind, Name: it.Name, Order: it.Order, Source: relative(tg, it.Source),
			Description: it.Description, Targets: it.Targets.Include, Activation: it.Activation, Scope: it.Scope,
			Meta: it.Meta, Files: files, Native: it.Native, Portable: it.Portable, MCP: it.MCP, Body: it.Body,
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
		n.Path = relative(tg, n.Path)
		view.Notes = append(view.Notes, n)
	}
	for _, u := range imported.Unresolved {
		u.File = relative(tg, u.File)
		view.Unresolved = append(view.Unresolved, u)
	}
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
			u.Kind, relative(tg, u.Path), u.Key, relative(tg, u.Root), u.Mode, strings.Join(items, ","), u.Content)
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

func libraryItems() []library.Item {
	return []library.Item{
		{ID: "ps_always", Kind: library.KindRule, Name: "Reviews", Order: 10, Body: "## Reviews\n\nBe critical.\n"},
		{ID: "ps_go", Kind: library.KindRule, Name: "Go style", Order: 20, Activation: library.ActivationFiles, Scope: []string{"**/*.go", "go.mod"}, Body: "## Go style\n\nUse tabs.\n"},
		{ID: "ps_agent", Kind: library.KindRule, Name: "Releases", Order: 30, Activation: library.ActivationAgent, Description: "Preparing a release", Body: "## Releases\n\nUpdate the changelog.\n"},
		{ID: "ps_claude", Kind: library.KindRule, Name: "Claude notes", Order: 40, Targets: library.Targets{Include: []string{adapter.ClaudeCode}}, Body: "## Claude notes\n\nPlan first.\n"},
		{ID: "ps_skill", Kind: library.KindSkill, Name: "release-notes", Description: "Draft release notes.", Body: "Summarise the changes.\n", Files: map[string]library.File{"scripts/run.sh": {Data: []byte("#!/bin/sh\n"), Mode: 0o755}}},
		{ID: "ps_command", Kind: library.KindCommand, Name: "review", Description: "Review a PR", Meta: map[string]any{"argument-hint": "[pr]", "name": "ignored", "paths": "ignored"}, Body: "Review PR $0.\n"},
		{ID: "ps_nested", Kind: library.KindCommand, Name: "git:commit", Body: "Write a commit message.\n"},
		{ID: "ps_agent_file", Kind: library.KindSubagent, Name: "code-reviewer", Description: "Reviews diffs.", Meta: map[string]any{"tools": "Read, Grep", "model": "sonnet"}, Body: "Be strict.\n"},
		{ID: "ps_perm", Kind: library.KindPermission, Name: "Guard", Portable: &library.PortablePermission{
			DenyCommands:    []string{"git push --force"},
			DenyPaths:       []string{"**/.env", "/etc/example/**"},
			AllowMCPServers: []string{"docs", "my.server"},
		}, Native: map[string]any{adapter.ClaudeCode: map[string]any{"ask": []any{"Bash(git commit *)"}, "deny": []any{"WebFetch(domain:example.org)"}}}},
		{ID: "ps_hook", Kind: library.KindHook, Name: "Guard edits", Native: map[string]any{adapter.ClaudeCode: map[string]any{
			"event": "PreToolUse", "matcher": "Edit|Write", "hooks": []any{map[string]any{"type": "command", "command": "~/.claude/hooks/guard.sh", "timeout": 30}},
		}}},
		{ID: "ps_mcp", Kind: library.KindMCP, Name: "docs", MCP: &library.MCPServer{
			Transport: "stdio", Command: "example-docs", Args: []string{"--verbose"},
			Env: map[string]library.Value{"DOCS_TOKEN": {Ref: "env:DOCS_TOKEN"}, "MODE": {Literal: "readonly"}},
		}},
		{ID: "ps_remote", Kind: library.KindMCP, Name: "remote", MCP: &library.MCPServer{
			Transport: "http", URL: "https://mcp.example.com/mcp",
			Headers: map[string]library.Value{"Authorization": {Literal: "Bearer ${env:REMOTE_TOKEN}"}},
		}},
	}
}

func TestDetect(t *testing.T) {
	a := registered(t)
	if a.ID() != adapter.ClaudeCode {
		t.Errorf("ID %q", a.ID())
	}
	env := testenv.Home(t, "testdata/basic/home")
	got := a.Detect(env)
	want := []string{filepath.Join(env.Home, ".claude"), filepath.Join(env.Home, ".claude.json")}
	if !got.Installed || !reflect.DeepEqual(got.Paths, want) {
		t.Errorf("detection %+v", got)
	}
	empty := env
	empty.Home = t.TempDir()
	if got := a.Detect(empty); got.Installed || len(got.Paths) != 0 {
		t.Errorf("empty home %+v", got)
	}
	empty.LookPath = func(name string) (string, error) { return "/usr/local/bin/" + name, nil }
	if got := a.Detect(empty); !got.Installed {
		t.Errorf("binary on PATH %+v", got)
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
	ref := ""
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
	tg := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	before := testenv.TreeHash(t, tg.Env.Home)
	imported := importFrom(t, tg)
	testenv.Golden(t, "testdata/golden/import-user.yaml", importYAML(t, tg, imported))
	if testenv.TreeHash(t, tg.Env.Home) != before {
		t.Error("import changed the home")
	}
}

func TestImportProjectScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.ClaudeCode)
	imported := importFrom(t, tg)
	testenv.Golden(t, "testdata/golden/import-project.yaml", importYAML(t, tg, imported))
}

func TestImportReadsAgentsMDWithoutClaudeMD(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.ClaudeCode)
	if err := os.Remove(filepath.Join(tg.ProjectRoot, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	build := find(t, importFrom(t, tg).Items, library.KindRule, "Build")
	if build.Source != filepath.Join(tg.ProjectRoot, "AGENTS.md") || len(build.Targets.Include) != 0 {
		t.Errorf("build %+v", build)
	}
}

func TestImportSplitsByH2(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	imported := importFrom(t, tg)
	var names []string
	for _, it := range imported.Items {
		if it.Kind == library.KindRule {
			names = append(names, it.Name+"@"+relative(tg, it.Source))
		}
	}
	want := []string{
		"CLAUDE@<home>/.claude/CLAUDE.md",
		"Reviews@<home>/.claude/CLAUDE.md",
		"Style@<home>/notes/style.md",
		"Testing@<home>/.claude/shared/testing.md",
		"cursor-style@<home>/.claude/rules/cursor-style.md",
		"Go@<home>/.claude/rules/go.md",
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("rules %v", names)
	}
	preamble := find(t, imported.Items, library.KindRule, "CLAUDE")
	if strings.Contains(preamble.Body, "@~/notes/style.md") || !strings.Contains(preamble.Body, "@never-imported.md") {
		t.Errorf("preamble %q", preamble.Body)
	}
	if reviews := find(t, imported.Items, library.KindRule, "Reviews"); reviews.Order != 20 {
		t.Errorf("reviews order %d", reviews.Order)
	}
	wantUnresolved := []adapter.Unresolved{{File: filepath.Join(tg.Env.Home, ".claude", "CLAUDE.md"), Line: "14", Target: "missing-guide.md", Reason: "missing"}}
	if !reflect.DeepEqual(imported.Unresolved, wantUnresolved) {
		t.Errorf("unresolved %+v", imported.Unresolved)
	}
}

func TestImportGlobsFrontmatterNoted(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	imported := importFrom(t, tg)
	want := adapter.Note{Path: filepath.Join(tg.Env.Home, ".claude", "rules", "cursor-style.md"), Message: "ignored by Claude Code; it reads only paths"}
	if !slices.Contains(imported.Notes, want) {
		t.Errorf("notes %+v", imported.Notes)
	}
	cursor := find(t, imported.Items, library.KindRule, "cursor-style")
	if cursor.Activation != "" || len(cursor.Scope) != 0 {
		t.Errorf("globs became scope %+v", cursor)
	}
	gorule := find(t, imported.Items, library.KindRule, "Go")
	if gorule.Activation != library.ActivationFiles || !reflect.DeepEqual(gorule.Scope, []string{"**/*.go", "go.mod"}) {
		t.Errorf("paths rule %+v", gorule)
	}
}

func TestImportMarksSyncedPluginAndBoostForeign(t *testing.T) {
	user := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	wantUser := []adapter.Foreign{
		{Path: filepath.Join(user.Env.Home, ".claude", "plugins"), Owner: "claude-code plugins", Reason: "installed plugins; Claude Code manages them"},
		{Path: filepath.Join(user.Env.Home, ".claude", "skills", "synced", "bucket-0001", "pdf-tools"), Owner: "claude.ai", Reason: "synced skill; Claude Code overwrites it"},
	}
	importedUser := importFrom(t, user)
	if !reflect.DeepEqual(importedUser.Foreign, wantUser) {
		t.Errorf("user foreign %+v", importedUser.Foreign)
	}
	for _, it := range importedUser.Items {
		if it.Name == "pdf-tools" {
			t.Errorf("synced skill imported %+v", it)
		}
	}
	project := target(t, "basic", adapter.ScopeProject, adapter.ClaudeCode)
	wantProject := []adapter.Foreign{
		{Path: filepath.Join(project.ProjectRoot, ".claude", "skills", "laravel-pest"), Owner: "laravel-boost", Reason: "listed in boost.json; Boost regenerates it"},
		{Path: filepath.Join(project.ProjectRoot, ".mcp.json"), Owner: "laravel-boost", Reason: "mcpServers/laravel-boost; Boost regenerates it"},
		{Path: filepath.Join(project.ProjectRoot, "CLAUDE.md"), Owner: "laravel-boost", Reason: "<laravel-boost-guidelines> block; Boost regenerates it"},
	}
	importedProject := importFrom(t, project)
	if !reflect.DeepEqual(importedProject.Foreign, wantProject) {
		t.Errorf("project foreign %+v", importedProject.Foreign)
	}
	for _, it := range importedProject.Items {
		if it.Name == "laravel-pest" || it.Name == "laravel-boost" || strings.Contains(it.Body, "Boost") {
			t.Errorf("boost content imported %+v", it)
		}
	}
}

func TestImportRuntimeGrantsFromSettingsLocal(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.ClaudeCode)
	local := filepath.Join(tg.ProjectRoot, ".claude", "settings.local.json")
	want := []adapter.Grant{
		{Tool: adapter.ClaudeCode, Path: local, Key: "/permissions/allow@=Bash(npm test *)", Value: "Bash(npm test *)"},
		{Tool: adapter.ClaudeCode, Path: local, Key: "/permissions/allow@=WebFetch(domain:docs.example.com)", Value: "WebFetch(domain:docs.example.com)"},
		{Tool: adapter.ClaudeCode, Path: local, Key: "/permissions/deny@=Read(./tmp/**)", Value: "Read(./tmp/**)"},
	}
	imported := importFrom(t, tg)
	if !reflect.DeepEqual(imported.Grants, want) {
		t.Errorf("grants %+v", imported.Grants)
	}
	perm := find(t, imported.Items, library.KindPermission, "Claude Code permissions")
	if !reflect.DeepEqual(perm.Portable.DenyCommands, []string{"curl"}) || len(perm.Portable.DenyPaths) != 0 {
		t.Errorf("project permission %+v", perm.Portable)
	}
	user := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	settings := filepath.Join(user.Env.Home, ".claude", "settings.json")
	wantUser := []adapter.Grant{
		{Tool: adapter.ClaudeCode, Path: settings, Key: "/permissions/allow@=Bash(make test *)", Value: "Bash(make test *)"},
		{Tool: adapter.ClaudeCode, Path: settings, Key: "/permissions/allow@=mcp__docs", Value: "mcp__docs"},
	}
	if got := importFrom(t, user).Grants; !reflect.DeepEqual(got, wantUser) {
		t.Errorf("user grants %+v", got)
	}
}

func TestSecretsScrubbedOnImport(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.ClaudeCode)
		report := importYAML(t, tg, importFrom(t, tg))
		for _, secret := range []string{"fixture-token-value", "fixture-bearer-value", "fixture-api-key", "fixture-secret-value", "fixture-user-id", "person@example.com", "00000000-0000"} {
			if bytes.Contains(report, []byte(secret)) {
				t.Errorf("%s import carries %s", scope, secret)
			}
		}
	}
	tg := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	docs := find(t, importFrom(t, tg).Items, library.KindMCP, "docs")
	if docs.MCP.Env["DOCS_TOKEN"] != (library.Value{Ref: "env:DOCS_TOKEN"}) || docs.MCP.Args[1] != "${env:TOKEN}" {
		t.Errorf("docs %+v", docs.MCP)
	}
}

func TestExportUserScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	units, unsupported := export(t, tg, libraryItems())
	testenv.Golden(t, "testdata/golden/units-user.txt", describe(tg, units))
	want := []adapter.Unsupported{{ItemID: "ps_agent", Name: "Releases", Tool: adapter.ClaudeCode, Reason: "use a skill"}}
	if !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}
	if _, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{}); err != nil {
		t.Errorf("units do not validate: %v", err)
	}
}

func TestExportProjectScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.ClaudeCode, adapter.Codex, adapter.AgentsMD)
	items := libraryItems()
	units, unsupported := export(t, tg, items)
	testenv.Golden(t, "testdata/golden/units-project.txt", describe(tg, units))
	if len(unsupported) != 0 {
		t.Errorf("unsupported %+v", unsupported)
	}
	if !reflect.DeepEqual(items, libraryItems()) {
		t.Error("export changed the items it was given")
	}
	if _, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{}); err != nil {
		t.Errorf("units do not validate: %v", err)
	}
}

func TestProjectClaudeMDImportsAgentsMD(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.ClaudeCode, adapter.Codex, adapter.AgentsMD)
	tg.ProjectRoot = t.TempDir()
	units, _ := export(t, tg, libraryItems()[:1])
	if len(units) != 1 || string(units[0].Content) != "@AGENTS.md\n" || units[0].Kind != manifest.UnitSection {
		t.Fatalf("units %s", describe(tg, units))
	}
	units, _ = export(t, tg, libraryItems()[3:4])
	if len(units) != 1 || string(units[0].Content) != "@AGENTS.md\n\n## Claude notes\n\nPlan first.\n" {
		t.Fatalf("units %s", describe(tg, units))
	}
	changes, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Op != plan.OpCreate {
		t.Fatalf("changes %+v", changes)
	}
	lines := strings.Split(string(changes[0].After), "\n")
	if len(lines) < 3 || lines[2] != "@AGENTS.md" {
		t.Errorf("CLAUDE.md %q", changes[0].After)
	}
	existing := filepath.Join(tg.ProjectRoot, "CLAUDE.md")
	if err := os.WriteFile(existing, []byte("# Mine\n\nKeep this.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changes, err = plan.Diff(units, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(changes[0].After, []byte("# Mine\n\nKeep this.\n")) {
		t.Errorf("user text not kept %q", changes[0].After)
	}
	if units, _ := export(t, tg, nil); len(units) != 0 {
		t.Errorf("CLAUDE.md written with no rules %s", describe(tg, units))
	}
}

func TestExportBashDenySyntax(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	items := []library.Item{{ID: "ps_perm", Kind: library.KindPermission, Name: "Guard", Portable: &library.PortablePermission{DenyCommands: []string{"git push --force", "rm -rf"}}}}
	units, unsupported := export(t, tg, items)
	if len(unsupported) != 0 {
		t.Errorf("unsupported %+v", unsupported)
	}
	var got []string
	for _, u := range units {
		got = append(got, u.Key+" "+string(u.Content))
	}
	want := []string{
		`/permissions/deny@=Bash(git push --force *) "Bash(git push --force *)"`,
		`/permissions/deny@=Bash(rm -rf *) "Bash(rm -rf *)"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("units %v", got)
	}
	for _, u := range units {
		if u.Kind != manifest.UnitJSONElement || u.Path != filepath.Join(tg.Env.Home, ".claude", "settings.json") || strings.Contains(u.Key, ":*") {
			t.Errorf("unit %+v", u)
		}
	}
}

func TestExportPathDenyUsesReadAndEdit(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	items := []library.Item{{ID: "ps_perm", Kind: library.KindPermission, Name: "Guard", Portable: &library.PortablePermission{DenyPaths: []string{"**/.env", "/etc/example/**", "~/.ssh/**"}}}}
	units, _ := export(t, tg, items)
	var got []string
	for _, u := range units {
		got = append(got, u.Key)
	}
	want := []string{
		"/permissions/deny@=Edit(**/.env)",
		"/permissions/deny@=Edit(//etc/example/**)",
		"/permissions/deny@=Edit(~/.ssh/**)",
		"/permissions/deny@=Read(**/.env)",
		"/permissions/deny@=Read(//etc/example/**)",
		"/permissions/deny@=Read(~/.ssh/**)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("keys %v", got)
	}
	for _, key := range got {
		if strings.Contains(key, "Write(") {
			t.Errorf("Write rule %s", key)
		}
	}
}

func TestExportSymlinkedSkillBlocked(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	items := []library.Item{{ID: "ps_linked", Kind: library.KindSkill, Name: "linked", Description: "Replaced.", Body: "New.\n"}}
	units, unsupported := export(t, tg, items)
	if len(unsupported) != 0 || len(units) != 1 {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	before := testenv.TreeHash(t, tg.Env.Home)
	changes, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !strings.HasPrefix(changes[0].Blocked, "symlinked -> ../../dotfiles/skills/linked") {
		t.Errorf("changes %+v", changes)
	}
	if testenv.TreeHash(t, tg.Env.Home) != before {
		t.Error("the home changed")
	}
}

func TestExportSkillErrorsAreUnsupported(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	items := []library.Item{
		{ID: "ps_bad", Kind: library.KindSkill, Name: "../escape", Body: "x\n"},
		{ID: "ps_cred", Kind: library.KindSkill, Name: "tokens", Body: "x\n", Files: map[string]library.File{"scripts/token.json": {Data: []byte("{}")}}},
	}
	units, unsupported := export(t, tg, items)
	if len(units) != 0 || len(unsupported) != 2 {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	for _, u := range unsupported {
		if u.Blocking || u.Tool != adapter.ClaudeCode || u.Reason == "" {
			t.Errorf("unsupported %+v", u)
		}
	}
}

func TestExportNeverTouchesOAuthAccount(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	path := filepath.Join(tg.Env.Home, ".claude.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	units, _ := export(t, tg, libraryItems())
	var changed *plan.FileChange
	changes, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{SharedState: claudecode.SharedStateFiles(tg.Env)})
	if err != nil {
		t.Fatal(err)
	}
	for i := range changes {
		if changes[i].Path == path {
			changed = &changes[i]
		}
	}
	if changed == nil || changed.Op != plan.OpUpdate || !changed.SharedState {
		t.Fatalf("change %+v", changed)
	}
	for _, u := range changed.Units {
		if !strings.HasPrefix(u.Key, "/mcpServers/") {
			t.Errorf("unit key %s", u.Key)
		}
	}
	for _, pointer := range []string{"/oauthAccount", "/userID", "/projects", "/numStartups", "/mcpServers/docs"} {
		was, _, err := structedit.JSONGet(before, pointer)
		if err != nil {
			t.Fatal(err)
		}
		now, _, err := structedit.JSONGet(changed.After, pointer)
		if err != nil {
			t.Fatal(err)
		}
		if pointer != "/mcpServers/docs" && !bytes.Equal(was, now) {
			t.Errorf("%s changed from %s to %s", pointer, was, now)
		}
	}
	if !bytes.Contains(changed.After, []byte(`"oauthAccount": {`)) {
		t.Error("oauthAccount formatting changed")
	}
	set := registered(t).BackupSet(tg)
	for _, entry := range set {
		if entry.Path == path && !reflect.DeepEqual(entry.Keys, []string{"/mcpServers"}) {
			t.Errorf("backup entry %+v", entry)
		}
	}
}

func TestBackupSet(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.ClaudeCode)
		var out strings.Builder
		for _, e := range registered(t).BackupSet(tg) {
			fmt.Fprintf(&out, "%s root=%s keys=%v redact=%v\n", relative(tg, e.Path), relative(tg, e.Root), e.Keys, e.RedactKeys)
		}
		testenv.Golden(t, "testdata/golden/backup-"+string(scope)+".txt", []byte(out.String()))
	}
}

func TestUnsupportedReported(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.ClaudeCode)
	items := []library.Item{
		{ID: "ps_agent", Kind: library.KindRule, Name: "Releases", Activation: library.ActivationAgent, Body: "## Releases\n"},
		{ID: "ps_hook", Kind: library.KindHook, Name: "Other tool", Native: map[string]any{"codex": map[string]any{}}},
		{ID: "ps_hook_http", Kind: library.KindHook, Name: "HTTP", Native: map[string]any{adapter.ClaudeCode: map[string]any{"event": "Stop", "hooks": []any{map[string]any{"type": "http", "url": "https://hooks.example.com"}}}}},
		{ID: "ps_sub", Kind: library.KindSubagent, Name: "nameless", Body: "x\n"},
		{ID: "ps_ws", Kind: library.KindMCP, Name: "ws", MCP: &library.MCPServer{Transport: "ws", URL: "wss://example.com"}},
		{ID: "ps_keychain", Kind: library.KindMCP, Name: "kc", MCP: &library.MCPServer{Transport: "stdio", Command: "x", Env: map[string]library.Value{"API_KEY": {Ref: "keychain:example"}}}},
		{ID: "ps_native", Kind: library.KindPermission, Name: "Odd", Native: map[string]any{adapter.ClaudeCode: map[string]any{"defaultMode": "plan"}}},
	}
	units, unsupported := export(t, tg, items)
	if len(units) != 0 {
		t.Errorf("units %s", describe(tg, units))
	}
	var got []string
	for _, u := range unsupported {
		if u.Tool != adapter.ClaudeCode || u.Blocking {
			t.Errorf("unsupported %+v", u)
		}
		got = append(got, string(u.ItemID)+": "+u.Reason)
	}
	testenv.Golden(t, "testdata/golden/unsupported.txt", []byte(strings.Join(got, "\n")+"\n"))
}

func TestNativeEnvRefOnExport(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.ClaudeCode)
		server := library.MCPServer{
			Transport: "http", URL: "https://mcp.example.com/mcp?region=${env:REGION}",
			Headers: map[string]library.Value{"Authorization": {Literal: "Bearer ${env:REMOTE_TOKEN}"}, "X-Key": {Ref: "env:REMOTE_KEY"}},
		}
		prepared, _, needsSetup, err := secrets.Prepare(server, registered(t).Capabilities().EnvRef, nil)
		if err != nil || len(needsSetup) != 0 {
			t.Fatalf("prepare %v %v", err, needsSetup)
		}
		for _, mcp := range []library.MCPServer{server, prepared} {
			units, unsupported := export(t, tg, []library.Item{{ID: "ps_remote", Kind: library.KindMCP, Name: "remote", MCP: &mcp}})
			if len(units) != 1 || len(unsupported) != 0 {
				t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
			}
			want := `{"type":"http","url":"https://mcp.example.com/mcp?region=${REGION}","headers":{"Authorization":"Bearer ${REMOTE_TOKEN}","X-Key":"${REMOTE_KEY}"}}`
			if string(units[0].Content) != want || units[0].Key != "/mcpServers/remote" {
				t.Errorf("%s unit %s", scope, describe(tg, units))
			}
		}
	}
}

func TestRoundTripNoChanges(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			tg := target(t, "roundtrip", scope, adapter.ClaudeCode, adapter.Codex, adapter.AgentsMD)
			homeBefore, projectBefore := testenv.TreeHash(t, tg.Env.Home), testenv.TreeHash(t, tg.ProjectRoot)
			imported := importFrom(t, tg)
			if len(imported.Items) == 0 || len(imported.Notes) != 0 || len(imported.Unresolved) != 0 {
				t.Fatalf("imported %s", importYAML(t, tg, imported))
			}
			units, unsupported := export(t, tg, imported.Items)
			if len(unsupported) != 0 {
				t.Errorf("unsupported %+v", unsupported)
			}
			changes, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{SharedState: claudecode.SharedStateFiles(tg.Env)})
			if err != nil {
				t.Fatal(err)
			}
			if len(changes) == 0 {
				t.Fatal("no changes planned")
			}
			for _, change := range changes {
				if change.Op != plan.OpNoop || change.Blocked != "" {
					t.Errorf("%s %s %q\n%s", change.Op, relative(tg, change.Path), change.Blocked, change.After)
				}
			}
			if testenv.TreeHash(t, tg.Env.Home) != homeBefore || testenv.TreeHash(t, tg.ProjectRoot) != projectBefore {
				t.Error("the fixture changed")
			}
		})
	}
}
