package antigravity_test

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
	_ "github.com/antonL95/promptstor/internal/adapter/antigravity"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	homeMarker    = "<home>"
	projectMarker = "<project>"

	appReason         = "not enforced in the Antigravity App and IDE"
	projectDenyReason = "Antigravity has no project permission file"
)

func registered(t *testing.T) adapter.Adapter {
	t.Helper()
	a, ok := adapter.Get(adapter.Antigravity)
	if !ok {
		t.Fatal("antigravity is not registered")
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
	return adapter.Target{Env: testenv.Home(t, t.TempDir()), Scope: scope, ProjectRoot: t.TempDir(), Selected: selected}
}

func relative(tg adapter.Target, path string) string {
	path = strings.Replace(path, tg.ProjectRoot, projectMarker, 1)
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
	changes, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.Antigravity}, plan.Options{})
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

func guard(commands, paths []string) library.Item {
	return library.Item{ID: "ps_perm", Kind: library.KindPermission, Name: "Guard", Portable: &library.PortablePermission{DenyCommands: commands, DenyPaths: paths}}
}

func server(id, name string, mcp library.MCPServer) library.Item {
	return library.Item{ID: library.ID(id), Kind: library.KindMCP, Name: name, MCP: &mcp}
}

func hook(id, name string, native map[string]any) library.Item {
	return library.Item{ID: library.ID(id), Kind: library.KindHook, Name: name, Native: map[string]any{adapter.Antigravity: native}}
}

func noteHook() map[string]any {
	return map[string]any{"SessionStart": []any{map[string]any{"type": "command", "command": "/opt/example/hooks/note.sh", "timeout": 10}}}
}

func unsupportedFor(id, name, reason string, blocking bool) adapter.Unsupported {
	return adapter.Unsupported{ItemID: library.ID(id), Name: name, Tool: adapter.Antigravity, Reason: reason, Blocking: blocking}
}

func libraryItems() []library.Item {
	only := rule("ps_only", "Antigravity notes", "")
	only.Targets = library.Targets{Include: []string{adapter.Antigravity}}
	releases := rule("ps_agent", "Releases", library.ActivationAgent)
	releases.Description = "Use when preparing a release."
	return []library.Item{
		rule("ps_always", "Reviews", library.ActivationAlways),
		rule("ps_go", "Go style", library.ActivationFiles, "**/*.go", "src/*.mod", "go.sum"),
		releases,
		only,
		{ID: "ps_skill", Kind: library.KindSkill, Name: "release-notes", Description: "Draft release notes.", Body: "Summarise the changes.\n", Files: map[string]library.File{"scripts/run.sh": {Data: []byte("#!/bin/sh\n"), Mode: 0o755}}},
		{ID: "ps_command", Kind: library.KindCommand, Name: "git:commit", Description: "Write a commit message", Body: "Write a commit message.\n"},
		{ID: "ps_sub", Kind: library.KindSubagent, Name: "code-reviewer", Description: "Reviews diffs.", Meta: map[string]any{
			"antigravity_model": "flash", "antigravity_tools": []any{"view_file"}, "antigravity_main_agent": false,
		}, Body: "Be strict.\n"},
		guard([]string{"git push --force *", "rm -rf"}, []string{"/etc/example/**", "~/.ssh"}),
		hook("ps_hook", "Session note", map[string]any{"SessionStart": []any{map[string]any{"type": "command", "command": "/opt/example/hooks/start.sh"}}}),
		server("ps_mcp", "tracker", library.MCPServer{Transport: "stdio", Command: "example-tracker", Args: []string{"--verbose"}, Env: map[string]library.Value{"MODE": {Literal: "readonly"}}}),
		server("ps_remote", "remote", library.MCPServer{Transport: "http", URL: "https://mcp.example.com/mcp", Headers: map[string]library.Value{"X-Team": {Literal: "a"}}}),
	}
}

func unitAt(t *testing.T, tg adapter.Target, units []plan.Unit, parts ...string) plan.Unit {
	t.Helper()
	path := filepath.Join(parts...)
	for _, u := range units {
		if u.Path == path {
			return u
		}
	}
	t.Fatalf("no unit at %s in %s", relative(tg, path), describe(tg, units))
	return plan.Unit{}
}

func TestDetect(t *testing.T) {
	a := registered(t)
	if a.ID() != adapter.Antigravity {
		t.Errorf("ID %q", a.ID())
	}
	for _, name := range []string{"antigravity", "antigravity-ide", "antigravity-cli"} {
		env := testenv.Home(t, t.TempDir())
		dir := filepath.Join(env.Home, ".gemini", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if got := a.Detect(env); !got.Installed || !reflect.DeepEqual(got.Paths, []string{dir}) {
			t.Errorf("%s detection %+v", name, got)
		}
	}
	env := testenv.Home(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(env.Home, ".gemini", "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := a.Detect(env); got.Installed || len(got.Paths) != 0 {
		t.Errorf("a config folder alone counted as installed %+v", got)
	}
	for _, binary := range []string{"agy", "antigravity"} {
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
	tg := target(t, "basic", adapter.ScopeUser, adapter.Antigravity)
	before := testenv.TreeHash(t, tg.Env.Home)
	imported := importFrom(t, tg)
	testenv.Golden(t, "testdata/golden/import-user.yaml", importYAML(t, tg, imported))
	if testenv.TreeHash(t, tg.Env.Home) != before {
		t.Error("import changed the home")
	}
}

func TestImportProjectScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.Antigravity, adapter.AgentsMD)
	testenv.Golden(t, "testdata/golden/import-project.yaml", importYAML(t, tg, importFrom(t, tg)))
}

func TestRuleIncludesImported(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Antigravity)
	imported := importFrom(t, tg)
	if len(imported.Unresolved) != 0 {
		t.Errorf("unresolved %+v", imported.Unresolved)
	}
	checklist := find(t, imported.Items, library.KindRule, "Checklist")
	if checklist.Source != filepath.Join(tg.Env.Home, ".gemini", "config", "checklists", "release.md") ||
		checklist.Activation != library.ActivationAgent || checklist.Description != "Use when preparing a release." ||
		checklist.Body != "## Checklist\n\nTag the release.\n" {
		t.Errorf("included rule %+v", checklist)
	}
	if style := find(t, imported.Items, library.KindRule, "Style notes"); style.Source != filepath.Join(tg.Env.Home, "notes", "style.md") || style.Activation != "" {
		t.Errorf("home include %+v", style)
	}
	if release := find(t, imported.Items, library.KindRule, "release"); strings.Contains(release.Body, "@[Release checklist]") || !strings.Contains(release.Body, "@[Fenced]") {
		t.Errorf("including rule %q", release.Body)
	}
	project := target(t, "basic", adapter.ScopeProject, adapter.Antigravity)
	notes := find(t, importFrom(t, project).Items, library.KindRule, "notes")
	if notes.Source != filepath.Join(project.ProjectRoot, ".agents", "notes.md") || !reflect.DeepEqual(notes.Targets.Include, []string{adapter.Antigravity}) {
		t.Errorf("project include %+v", notes)
	}
}

func TestUnreadableIncludesReported(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
	rules := filepath.Join(tg.Env.Home, ".gemini", "config", "rules")
	main := filepath.Join(rules, "main.md")
	write(t, main, "---\ntrigger: always_on\n---\n@[Gone](gone.md)\n@[Key](~/.codex/auth.json)\n@[Broken](../broken.md)\nKept.\n")
	write(t, filepath.Join(tg.Env.Home, ".codex", "auth.json"), "fixture-secret-value\n")
	write(t, filepath.Join(tg.Env.Home, ".gemini", "config", "broken.md"), "---\nunclosed\n")
	imported := importFrom(t, tg)
	want := []adapter.Unresolved{
		{File: main, Line: "4", Target: "[Gone](gone.md)", Reason: "missing"},
		{File: main, Line: "5", Target: "[Key](~/.codex/auth.json)", Reason: "credential file"},
	}
	if !reflect.DeepEqual(imported.Unresolved, want) {
		t.Errorf("unresolved %+v", imported.Unresolved)
	}
	if bytes.Contains(importYAML(t, tg, imported), []byte("fixture-secret-value")) {
		t.Error("import carries a credential")
	}
	broken := filepath.Join(tg.Env.Home, ".gemini", "config", "broken.md")
	if len(imported.Notes) != 1 || imported.Notes[0].Path != broken {
		t.Errorf("notes %+v", imported.Notes)
	}
	if len(imported.Items) != 1 || imported.Items[0].Body != "Kept.\n" {
		t.Errorf("items %+v", imported.Items)
	}
}

func TestSharedGeminiMemoryNoted(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Antigravity)
	imported := importFrom(t, tg)
	memory := filepath.Join(tg.Env.Home, ".gemini", "GEMINI.md")
	if want := (adapter.Note{Path: memory, Message: "shared with Gemini CLI"}); !slices.Contains(imported.Notes, want) {
		t.Errorf("notes %+v", imported.Notes)
	}
	if got := find(t, imported.Items, library.KindRule, "Reviews"); got.Source != memory {
		t.Errorf("rule %+v", got)
	}
}

func TestImporterPluginsForeignWithWarning(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Antigravity)
	imported := importFrom(t, tg)
	gemini := filepath.Join(tg.Env.Home, ".gemini")
	var foreign []string
	for _, f := range imported.Foreign {
		foreign = append(foreign, f.Path)
	}
	want := []string{
		filepath.Join(gemini, "antigravity", "builtin"),
		filepath.Join(gemini, "config", "import_manifest.json"),
		filepath.Join(gemini, "config", "plugins", "migrated-settings"),
	}
	if !reflect.DeepEqual(foreign, want) {
		t.Errorf("foreign %v", foreign)
	}
	warning := adapter.Note{Path: filepath.Join(gemini, "config", "plugins"), Message: "plugins may duplicate items PromptStor manages and may hold literal secrets; not read"}
	if !slices.Contains(imported.Notes, warning) {
		t.Errorf("notes %+v", imported.Notes)
	}
	report := importYAML(t, tg, imported)
	for _, leaked := range []string{"fixture-plugin-secret", "COPIED_TOKEN", "Copied from another tool", "Built in sample"} {
		if bytes.Contains(report, []byte(leaked)) {
			t.Errorf("import carries %s", leaked)
		}
	}
}

func TestSecretsScrubbedOnImport(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Antigravity)
	imported := importFrom(t, tg)
	report := importYAML(t, tg, imported)
	for _, secret := range []string{"fixture-token-value", "fixture-bearer-value", "fixture-secret-value", "fixture-agent-secret", "fixture-plugin-secret"} {
		if bytes.Contains(report, []byte(secret)) {
			t.Errorf("import carries %s", secret)
		}
	}
	if docs := find(t, imported.Items, library.KindMCP, "docs"); docs.MCP.Env["MODE"] != (library.Value{Literal: "readonly"}) || docs.MCP.Env["DOCS_TOKEN"].Literal != "" {
		t.Errorf("docs %+v", docs.MCP)
	}
}

func TestExportUserScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Antigravity)
	items := libraryItems()
	units, unsupported := export(t, tg, items...)
	testenv.Golden(t, "testdata/golden/units-user.txt", describe(tg, units))
	if want := []adapter.Unsupported{unsupportedFor("ps_perm", "Guard", appReason, true)}; !reflect.DeepEqual(unsupported, want) {
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
	link := "symlinked -> ../../dotfiles/skills (parent directory <home>/.gemini/config/skills)"
	for _, path := range []string{"git-commit/SKILL.md", "release-notes/SKILL.md", "release-notes/scripts/run.sh"} {
		full := "<home>/.gemini/config/skills/" + path
		if got := relative(tg, blocked[full]); got != link {
			t.Errorf("%s blocked %q", path, got)
		}
		delete(blocked, full)
	}
	if want := map[string]string{"<home>/.gemini/config/rules/go-style.md": "not created by promptstor"}; !maps.Equal(blocked, want) {
		t.Errorf("blocked %v", blocked)
	}
	if testenv.TreeHash(t, tg.Env.Home) != before {
		t.Error("the home changed")
	}
}

func TestExportOntoEmptyHome(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
	units, _ := export(t, tg, libraryItems()...)
	for _, change := range diff(t, units) {
		if change.Op != plan.OpCreate || change.Blocked != "" {
			t.Errorf("%s %s %q", change.Op, relative(tg, change.Path), change.Blocked)
		}
		if strings.HasSuffix(change.Path, ".json") {
			var doc map[string]any
			if err := json.Unmarshal(change.After, &doc); err != nil {
				t.Errorf("%s %v\n%s", relative(tg, change.Path), err, change.After)
			}
		}
	}
}

func TestExportProjectScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.ClaudeCode, adapter.Antigravity, adapter.AgentsMD)
	tg.ProjectRoot = t.TempDir()
	scoped := rule("ps_scoped", "Only here", library.ActivationAlways)
	scoped.Targets = library.Targets{Include: []string{adapter.Antigravity}}
	releases := rule("ps_agent", "Releases", library.ActivationAgent)
	releases.Description = "Use when preparing a release."
	releases.Targets = library.Targets{Include: []string{adapter.Antigravity}}
	items := append(libraryItems(), scoped, releases)
	units, unsupported := export(t, tg, items...)
	testenv.Golden(t, "testdata/golden/units-project.txt", describe(tg, units))
	want := []adapter.Unsupported{
		unsupportedFor("ps_command", "git:commit", "Antigravity commands become skills, and .agents/skills is shared with other tools", false),
		unsupportedFor("ps_perm", "Guard", projectDenyReason, true),
		unsupportedFor("ps_hook", "Session note", "PromptStor writes Antigravity hooks at user scope only", false),
	}
	if !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}
	for _, change := range diff(t, units) {
		if change.Blocked != "" {
			t.Errorf("%s blocked %q", relative(tg, change.Path), change.Blocked)
		}
	}
}

func TestNoDoubleLoadWithGemini(t *testing.T) {
	both := rule("ps_both", "Both", library.ActivationAlways)
	excluded := rule("ps_mine", "Mine", "")
	excluded.Targets = library.Targets{Exclude: []string{adapter.GeminiCLI}}
	scoped := rule("ps_go", "Go style", library.ActivationFiles, "**/*.go")
	agent := rule("ps_agent", "Releases", library.ActivationAgent)
	agent.Description = "Releases."
	items := []library.Item{both, excluded, scoped, agent}
	rulePaths := func(tg adapter.Target) []string {
		units, unsupported := export(t, tg, items...)
		if len(unsupported) != 0 {
			t.Errorf("unsupported %+v", unsupported)
		}
		var names []string
		for _, u := range units {
			if filepath.Dir(u.Path) != filepath.Join(tg.Env.Home, ".gemini", "config", "rules") || u.Kind != manifest.UnitFile {
				t.Errorf("unit %s", u.Path)
			}
			names = append(names, filepath.Base(u.Path))
		}
		return names
	}
	with := emptyTarget(t, adapter.ScopeUser, adapter.GeminiCLI, adapter.Antigravity)
	if got := rulePaths(with); !reflect.DeepEqual(got, []string{"go-style.md", "mine.md", "releases.md"}) {
		t.Errorf("with gemini-cli %v", got)
	}
	without := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity, adapter.ClaudeCode)
	if got := rulePaths(without); !reflect.DeepEqual(got, []string{"both.md", "go-style.md", "mine.md", "releases.md"}) {
		t.Errorf("without gemini-cli %v", got)
	}
	write(t, filepath.Join(with.Env.Home, ".gemini", "settings.json"), `{"context":{"fileName":"AGENTS.md"}}`)
	if got := rulePaths(with); len(got) != 3 {
		t.Errorf("gemini memory in AGENTS.md %v", got)
	}
}

func TestGeminiMemoryAntigravityCannotRead(t *testing.T) {
	for _, settings := range []string{`{"context":{"fileName":"CONTEXT.md"}}`, `{"context":`} {
		tg := emptyTarget(t, adapter.ScopeUser, adapter.GeminiCLI, adapter.Antigravity)
		write(t, filepath.Join(tg.Env.Home, ".gemini", "settings.json"), settings)
		units, unsupported := export(t, tg, rule("ps_both", "Both", ""))
		if len(units) != 1 || len(unsupported) != 0 || filepath.Base(units[0].Path) != "both.md" {
			t.Errorf("%s units %s unsupported %+v", settings, describe(tg, units), unsupported)
		}
	}
	tg := emptyTarget(t, adapter.ScopeUser, adapter.GeminiCLI, adapter.Antigravity)
	write(t, filepath.Join(tg.Env.Home, "dotfiles", "GEMINI.md"), "mine\n")
	if err := os.MkdirAll(filepath.Join(tg.Env.Home, ".gemini"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../dotfiles/GEMINI.md", filepath.Join(tg.Env.Home, ".gemini", "GEMINI.md")); err != nil {
		t.Fatal(err)
	}
	units, unsupported := export(t, tg, rule("ps_both", "Both", ""))
	want := []adapter.Unsupported{unsupportedFor("ps_both", "Both", "relies on Gemini CLI's memory file, which is behind a symlink and cannot be written", false)}
	if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestTriggerMapping(t *testing.T) {
	cases := []struct {
		item library.Item
		want string
	}{
		{rule("a", "One", ""), "---\ntrigger: always_on\n---\n"},
		{rule("b", "Two", library.ActivationAlways), "---\ntrigger: always_on\n---\n"},
		{rule("c", "Three", library.ActivationFiles, "*.ts", "*.tsx"), "---\ntrigger: glob\nglobs: '*.ts, *.tsx'\n---\n"},
		{rule("d", "Four", "", "web/**/*.vue", "/srv/app/*.go"), "---\ntrigger: glob\nglobs: '**/web/**/*.vue, /srv/app/*.go'\n---\n"},
		{func() library.Item {
			it := rule("e", "Five", library.ActivationAgent)
			it.Description = "When: releasing"
			return it
		}(), "---\ntrigger: model_decision\ndescription: 'When: releasing'\n---\n"},
	}
	for _, c := range cases {
		tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
		units, unsupported := export(t, tg, c.item)
		if len(units) != 1 || len(unsupported) != 0 {
			t.Fatalf("%s units %s unsupported %+v", c.item.Name, describe(tg, units), unsupported)
		}
		want := c.want + managed.Notice + "\n" + c.item.Body
		if got := string(units[0].Content); got != want {
			t.Errorf("%s content %q want %q", c.item.Name, got, want)
		}
	}
}

func TestInvalidTriggerRefused(t *testing.T) {
	cases := []struct {
		item   library.Item
		reason string
	}{
		{rule("a", "Manual", "manual"), `activation "manual" has no Antigravity trigger`},
		{rule("b", "Camel", "alwaysOn"), `activation "alwaysOn" has no Antigravity trigger`},
		{rule("c", "No globs", library.ActivationFiles), "Antigravity needs globs for a glob rule"},
		{rule("d", "No summary", library.ActivationAgent), "Antigravity needs a description for a model_decision rule"},
		{rule("e", "Braces", library.ActivationFiles, "*.{ts,tsx}"), `glob "*.{ts,tsx}" cannot be written; Antigravity takes comma separated globs without braces`},
		{rule("f", "!!", ""), "the name has no characters usable in a file name"},
	}
	for _, c := range cases {
		tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
		units, unsupported := export(t, tg, c.item)
		want := []adapter.Unsupported{unsupportedFor(string(c.item.ID), c.item.Name, c.reason, false)}
		if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
			t.Errorf("%s units %s unsupported %+v", c.item.Name, describe(tg, units), unsupported)
		}
	}
}

func TestSameRuleSlugKeepsBothFiles(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
	units, _ := export(t, tg, rule("a", "Notes", ""), rule("b", "notes", ""))
	var names []string
	for _, u := range units {
		names = append(names, filepath.Base(u.Path))
	}
	if !reflect.DeepEqual(names, []string{"notes-2.md", "notes.md"}) {
		t.Errorf("names %v", names)
	}
}

func TestSizeWarnings(t *testing.T) {
	sized := func(id, name string, size int, activation library.Activation) library.Item {
		it := rule(id, name, activation)
		it.Body = strings.Repeat("a", size) + "\n"
		it.Description = "Large."
		return it
	}
	tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
	units, unsupported := export(t, tg, sized("ps_big", "Big", 24000, library.ActivationAgent))
	size := len(units[0].Content)
	want := []adapter.Unsupported{unsupportedFor("ps_big", "Big", fmt.Sprintf("the rule file is %d bytes; Antigravity truncates it after 24000", size), false)}
	if len(units) != 1 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}
	var items []library.Item
	for i := range 4 {
		items = append(items, sized(fmt.Sprintf("ps_%d", i), fmt.Sprintf("Rule %d", i), 20000+i, ""))
	}
	units, unsupported = export(t, tg, items...)
	total := 0
	for _, u := range units {
		total += len(u.Content)
	}
	want = []adapter.Unsupported{unsupportedFor("ps_3", "Rule 3", fmt.Sprintf("always-on rules come to about %d tokens; Antigravity demotes the largest above 20000", total/4), false)}
	if len(units) != 4 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %d unsupported %+v", len(units), unsupported)
	}
	if units, unsupported = export(t, tg, items[:3]...); len(units) != 3 || len(unsupported) != 0 {
		t.Errorf("under budget %+v", unsupported)
	}
	shared := emptyTarget(t, adapter.ScopeUser, adapter.GeminiCLI, adapter.Antigravity)
	if units, unsupported = export(t, shared, items...); len(units) != 0 || len(unsupported) != 1 {
		t.Errorf("rules in GEMINI.md are not counted: %+v", unsupported)
	}
}

func TestCLIDenyWrittenAndAppAckRequired(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
	units, unsupported := export(t, tg, guard([]string{"git push --force *", "rm -rf"}, []string{"/etc/example/**", "~/.ssh"}))
	if want := []adapter.Unsupported{unsupportedFor("ps_perm", "Guard", appReason, true)}; !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}
	settings := filepath.Join(tg.Env.Home, ".gemini", "antigravity-cli", "settings.json")
	var rules []string
	for _, u := range units {
		var value string
		if err := json.Unmarshal(u.Content, &value); err != nil {
			t.Fatal(err)
		}
		if u.Path != settings || u.Kind != manifest.UnitJSONElement || u.Key != "/permissions/deny@="+value {
			t.Errorf("unit %s", describe(tg, units))
		}
		rules = append(rules, relative(tg, value))
	}
	want := []string{
		"command(git push --force)", "command(rm -rf)",
		"read_file(/etc/example)", "read_file(<home>/.ssh)",
		"write_file(/etc/example)", "write_file(<home>/.ssh)",
	}
	if !reflect.DeepEqual(rules, want) {
		t.Errorf("rules %v", rules)
	}
	write(t, settings, "{\n  \"model\": \"example-model\",\n  \"permissions\": { \"allow\": [\"command(git)\"], \"deny\": [\"command(sudo)\"] }\n}\n")
	changes := diff(t, units)
	if len(changes) != 1 || changes[0].Blocked != "" {
		t.Fatalf("changes %+v", changes)
	}
	for _, kept := range []string{`"model": "example-model"`, `"allow": ["command(git)"]`, `"command(sudo)"`, `"command(rm -rf)"`} {
		if !bytes.Contains(changes[0].After, []byte(kept)) {
			t.Errorf("missing %s in %s", kept, changes[0].After)
		}
	}
}

func TestRelativeDenyPathReported(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
	units, unsupported := export(t, tg, guard(nil, []string{"secrets/**"}))
	want := []adapter.Unsupported{
		unsupportedFor("ps_perm", "Guard", `deny path "secrets/**" is matched from the workspace root only`, true),
		unsupportedFor("ps_perm", "Guard", appReason, true),
	}
	if len(units) != 2 || units[0].Key != "/permissions/deny@=read_file(secrets)" || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestInexpressibleDeniesBlocked(t *testing.T) {
	check := func(label string, item library.Item) {
		tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
		units, unsupported := export(t, tg, item)
		if len(units) != 0 || len(unsupported) != 1 || !unsupported[0].Blocking {
			t.Errorf("%q units %s unsupported %+v", label, describe(tg, units), unsupported)
		}
	}
	for _, command := range []string{"git * --force", "npm*", `rm "my file"`, "echo 'x'", "a && b", "*", "  ", "regex:.*", "echo $(id)"} {
		check(command, guard([]string{command}, nil))
	}
	for _, path := range []string{"", "/", "**", "/**", "**/.env", "secrets/*.pem", "a/**/b", "!keep", "../out", "./here", "~", "~other/x", "a{b,c}", "a[0]", "a?", "a(1)", "a\nb"} {
		check(path, guard(nil, []string{path}))
	}
}

func TestDenyPathsUnderHomeStayPortable(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
	denied := tg.Env.Home + "/.ssh"
	write(t, filepath.Join(tg.Env.Home, ".gemini", "antigravity-cli", "settings.json"),
		fmt.Sprintf(`{"permissions":{"deny":["read_file(%s)","write_file(%s)","read_file(/srv/a/**)"]}}`, denied, denied))
	imported := importFrom(t, tg)
	permission := find(t, imported.Items, library.KindPermission, "Antigravity permissions")
	if !reflect.DeepEqual(permission.Portable.DenyPaths, []string{"~/.ssh"}) {
		t.Errorf("deny paths %v", permission.Portable.DenyPaths)
	}
	if len(imported.Notes) != 1 || !strings.Contains(imported.Notes[0].Message, "read_file(/srv/a/**)") {
		t.Errorf("notes %+v", imported.Notes)
	}
	units, _ := export(t, tg, permission)
	for _, change := range diff(t, units) {
		if change.Op != plan.OpNoop || change.Blocked != "" {
			t.Errorf("%s %q\n%s", change.Op, change.Blocked, change.After)
		}
	}
}

func TestProjectDeniesBlocked(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeProject, adapter.Antigravity)
	units, unsupported := export(t, tg, guard([]string{"rm -rf"}, nil))
	if want := []adapter.Unsupported{unsupportedFor("ps_perm", "Guard", projectDenyReason, true)}; len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestHooksKeyedByName(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Antigravity)
	imported := importFrom(t, tg)
	guarded := find(t, imported.Items, library.KindHook, "guard-writes")
	if guarded.Native[adapter.Antigravity].(map[string]any)["enabled"] != false {
		t.Errorf("hook %+v", guarded.Native)
	}
	find(t, imported.Items, library.KindHook, "session-note")
	other := noteHook()
	other["enabled"] = true
	units, unsupported := export(t, tg, hook("ps_hook", "Guard edits", other))
	if len(units) != 1 || len(unsupported) != 0 {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	u := units[0]
	hooks := filepath.Join(tg.Env.Home, ".gemini", "config", "hooks.json")
	if u.Path != hooks || u.Kind != manifest.UnitJSON || u.Key != "/promptstor-guard-edits" {
		t.Errorf("unit %s", describe(tg, units))
	}
	changes := diff(t, units)
	var doc map[string]any
	if err := json.Unmarshal(changes[0].After, &doc); err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(doc)); !reflect.DeepEqual(got, []string{"promptstor-guard-edits", "promptstor-guard-writes", "session-note"}) {
		t.Errorf("hooks %v", got)
	}
}

func TestHookAlreadyPresentNotDuplicated(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Antigravity)
	units, unsupported := export(t, tg, hook("ps_hook", "session-note", noteHook()))
	want := []adapter.Unsupported{unsupportedFor("ps_hook", "session-note", `hooks.json already has this hook as "session-note"; remove it there to let PromptStor manage it`, false)}
	if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestInvalidHooksReported(t *testing.T) {
	handler := map[string]any{"command": "x"}
	for label, native := range map[string]map[string]any{
		"no event":      {"enabled": true},
		"unknown event": {"BeforeTool": []any{handler}},
		"not a list":    {"Stop": handler},
		"empty list":    {"Stop": []any{}},
		"flat grouped":  {"PreToolUse": []any{handler}},
		"grouped flat":  {"Stop": []any{map[string]any{"hooks": []any{handler}}}},
		"no command":    {"Stop": []any{map[string]any{"type": "command"}}},
		"other type":    {"Stop": []any{map[string]any{"type": "prompt", "command": "x"}}},
		"enabled text":  {"enabled": "no", "Stop": []any{handler}},
	} {
		tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
		units, unsupported := export(t, tg, hook("ps_hook", "Bad", native))
		if len(units) != 0 || len(unsupported) != 1 || unsupported[0].Blocking {
			t.Errorf("%s units %s unsupported %+v", label, describe(tg, units), unsupported)
		}
	}
}

func TestExportKeepsServerControlSettings(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Antigravity)
	docs := server("ps_docs", "docs", library.MCPServer{Transport: "stdio", Command: "example-docs", Args: []string{"--stdio"}})
	units, unsupported := export(t, tg, docs)
	want := `{"command":"example-docs","args":["--stdio"],"disabled":false,"timeoutSeconds":600}`
	if len(units) != 1 || len(unsupported) != 0 || string(units[0].Content) != want || units[0].Key != "/mcpServers/docs" {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	signed := server("ps_signed", "signed", library.MCPServer{Transport: "http", URL: "https://signed.example.com/mcp"})
	units, unsupported = export(t, tg, signed)
	reasons := []adapter.Unsupported{unsupportedFor("ps_signed", "signed", "mcpServers/signed has oauth, which PromptStor would drop; set the server up by hand", false)}
	if len(units) != 0 || !reflect.DeepEqual(unsupported, reasons) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestEnvRefsNeverWritten(t *testing.T) {
	if registered(t).Capabilities().EnvRef != nil {
		t.Fatal("env interpolation in mcp_config.json is unconfirmed")
	}
	for _, mcp := range []library.MCPServer{
		{Transport: "stdio", Command: "x", Env: map[string]library.Value{"API_KEY": {Ref: "env:API_KEY"}}},
		{Transport: "stdio", Command: "x", Args: []string{"--key=${env:API_KEY}"}},
		{Transport: "http", URL: "https://example.com?r=${env:REGION}"},
		{Transport: "http", URL: "https://example.com", Headers: map[string]library.Value{"Authorization": {Literal: "Bearer ${env:TOKEN}"}}},
		{Transport: "stdio", Command: "x", Env: map[string]library.Value{"API_KEY": {Ref: "keychain:example"}}},
	} {
		tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
		units, unsupported := export(t, tg, server("ps_mcp", "s", mcp))
		if len(units) != 0 || len(unsupported) != 1 {
			t.Errorf("%+v units %s unsupported %+v", mcp, describe(tg, units), unsupported)
		}
	}
}

func TestCommandsAsSkills(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
	command := library.Item{ID: "ps_cmd", Kind: library.KindCommand, Name: "git:Commit All", Description: "Commit.", Meta: map[string]any{"argument-hint": "[scope]"}, Body: "Commit.\n"}
	units, unsupported := export(t, tg, command)
	want := []adapter.Unsupported{unsupportedFor("ps_cmd", "git:Commit All", "written without argument-hint, which Antigravity skill files do not take", false)}
	if len(units) != 1 || !reflect.DeepEqual(unsupported, want) {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	u := unitAt(t, tg, units, tg.Env.Home, ".gemini", "config", "skills", "git-commit-all", "SKILL.md")
	if got, want := string(u.Content), "---\nname: git-commit-all\ndescription: Commit.\n---\n"+managed.Notice+"\nCommit.\n"; got != want {
		t.Errorf("content %q", got)
	}
}

func TestSubagentFrontmatter(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
	item := library.Item{ID: "ps_sub", Kind: library.KindSubagent, Name: "Code Reader", Description: "Reads.", Meta: map[string]any{
		"model": "sonnet", "tools": "Read", "antigravity_model": "huge", "antigravity_subagent": true,
		"antigravity_command_execution_policy": "off", "antigravity_skills": []any{"skills/review"},
	}, Body: "Read.\n"}
	units, unsupported := export(t, tg, item)
	want := []adapter.Unsupported{unsupportedFor("ps_sub", "Code Reader", "written without antigravity_model, model, tools, which Antigravity agent files do not take", false)}
	if len(units) != 1 || !reflect.DeepEqual(unsupported, want) {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	content := "---\nname: code-reader\ndescription: Reads.\nsubagent: true\ncommandExecutionPolicy: \"off\"\nskills:\n  - skills/review\n---\n" + managed.Notice + "\nRead.\n"
	if got := string(units[0].Content); got != content {
		t.Errorf("content %q", got)
	}
}

func TestSubagentFolderFormNotDuplicated(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Antigravity)
	units, unsupported := export(t, tg, library.Item{ID: "ps_sub", Kind: library.KindSubagent, Name: "auditor", Description: "Audits.", Body: "Audit.\n"})
	want := []adapter.Unsupported{unsupportedFor("ps_sub", "auditor", "agents/auditor/agent.md already defines this agent; not written", false)}
	if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestUnsupportedReported(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser, adapter.Antigravity)
	items := []library.Item{
		{ID: "ps_skill", Kind: library.KindSkill, Name: "../escape", Description: "x", Body: "x\n"},
		{ID: "ps_nodesc", Kind: library.KindSkill, Name: "quiet", Body: "x\n"},
		{ID: "ps_cred", Kind: library.KindSkill, Name: "keys", Description: "x", Body: "x\n", Files: map[string]library.File{"scripts/token.json": {Data: []byte("{}")}}},
		{ID: "ps_cmd", Kind: library.KindCommand, Name: "mute", Description: "x", Body: "  "},
		{ID: "ps_cmdname", Kind: library.KindCommand, Name: "!!", Description: "x", Body: "x\n"},
		{ID: "ps_cmddesc", Kind: library.KindCommand, Name: "vague", Body: "x\n"},
		{ID: "ps_sub", Kind: library.KindSubagent, Name: "nameless", Body: "x\n"},
		{ID: "ps_builtin", Kind: library.KindSubagent, Name: "research", Description: "x", Body: "x\n"},
		{ID: "ps_allow", Kind: library.KindPermission, Name: "Servers", Portable: &library.PortablePermission{AllowMCPServers: []string{"docs"}}},
		{ID: "ps_native", Kind: library.KindPermission, Name: "Native", Native: map[string]any{adapter.Antigravity: map[string]any{}}},
		{ID: "ps_foreign", Kind: library.KindPermission, Name: "Claude only", Native: map[string]any{adapter.ClaudeCode: map[string]any{}}},
		{ID: "ps_hook", Kind: library.KindHook, Name: "Other tool", Native: map[string]any{adapter.ClaudeCode: map[string]any{}}},
		hook("ps_hookname", "!!", noteHook()),
		{ID: "ps_nomcp", Kind: library.KindMCP, Name: "empty"},
		server("ps_sse", "sse", library.MCPServer{Transport: "sse", URL: "https://example.com/sse"}),
		server("ps_both", "both", library.MCPServer{Transport: "http", URL: "https://example.com", Command: "x"}),
		server("ps_bare", "bare", library.MCPServer{Transport: "stdio"}),
	}
	units, unsupported := export(t, tg, items...)
	if len(units) != 0 {
		t.Errorf("units %s", describe(tg, units))
	}
	var got []string
	for _, u := range unsupported {
		if u.Tool != adapter.Antigravity || u.Blocking {
			t.Errorf("unsupported %+v", u)
		}
		got = append(got, string(u.ItemID)+": "+u.Reason)
	}
	testenv.Golden(t, "testdata/golden/unsupported.txt", []byte(strings.Join(got, "\n")+"\n"))
}

func TestBackupSet(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.Antigravity)
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
			tg := target(t, "roundtrip", scope, adapter.Antigravity, adapter.AgentsMD)
			homeBefore, projectBefore := testenv.TreeHash(t, tg.Env.Home), testenv.TreeHash(t, tg.ProjectRoot)
			imported := importFrom(t, tg)
			if len(imported.Items) == 0 || len(imported.Notes) != 0 || len(imported.Unresolved) != 0 {
				t.Fatalf("imported %s", importYAML(t, tg, imported))
			}
			units, unsupported := export(t, tg, imported.Items...)
			for _, u := range unsupported {
				if u.Reason != appReason {
					t.Errorf("unsupported %+v", u)
				}
			}
			changes := diff(t, units)
			if len(changes) == 0 {
				t.Fatal("no changes planned")
			}
			for _, change := range changes {
				if change.Op != plan.OpNoop || change.Blocked != "" {
					t.Errorf("%s %s %q\n%s", change.Op, relative(tg, change.Path), change.Blocked, change.After)
				}
			}
			var written []string
			for _, u := range units {
				written = append(written, u.Path+" "+u.Key)
			}
			wantUnits := map[adapter.Scope]int{adapter.ScopeUser: 11, adapter.ScopeProject: 4}
			if len(written) != wantUnits[scope] {
				t.Errorf("units %d\n%s", len(written), describe(tg, units))
			}
			if testenv.TreeHash(t, tg.Env.Home) != homeBefore || testenv.TreeHash(t, tg.ProjectRoot) != projectBefore {
				t.Error("the fixture changed")
			}
		})
	}
}
