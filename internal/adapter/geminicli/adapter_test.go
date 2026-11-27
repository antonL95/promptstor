package geminicli_test

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"

	"github.com/antonL95/promptstor/internal/adapter"
	_ "github.com/antonL95/promptstor/internal/adapter/geminicli"
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

	agentsOnlyReason  = "Gemini CLI reads only AGENTS.md and GEMINI.md in a project"
	userFileReason    = "context.fileName set by user"
	projectDenyReason = "project policy files are ignored by Gemini CLI"
	partialPathReason = "Gemini CLI path denies match only the file_path text of read_file, write_file and replace; shell, search and bulk read tools are not covered"
	noAllowListReason = "mcp.allowed is not set, and writing it would block every other MCP server"
)

func registered(t *testing.T) adapter.Adapter {
	t.Helper()
	a, ok := adapter.Get(adapter.GeminiCLI)
	if !ok {
		t.Fatal("gemini-cli is not registered")
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

func diff(t *testing.T, units []plan.Unit) []plan.FileChange {
	t.Helper()
	changes, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.GeminiCLI}, plan.Options{})
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
			Targets: it.Targets.Include, Meta: it.Meta, Files: files, Native: it.Native, Portable: it.Portable, MCP: it.MCP, Body: it.Body,
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

func guard(commands, paths []string) library.Item {
	return library.Item{ID: "ps_perm", Kind: library.KindPermission, Name: "Guard", Portable: &library.PortablePermission{DenyCommands: commands, DenyPaths: paths}}
}

func allow(servers ...string) library.Item {
	return library.Item{ID: "ps_allow", Kind: library.KindPermission, Name: "Servers", Portable: &library.PortablePermission{AllowMCPServers: servers}}
}

func server(id, name string, mcp library.MCPServer) library.Item {
	return library.Item{ID: library.ID(id), Kind: library.KindMCP, Name: name, MCP: &mcp}
}

func libraryItems() []library.Item {
	return []library.Item{
		{ID: "ps_always", Kind: library.KindRule, Name: "Reviews", Order: 10, Body: "## Reviews\n\nBe critical.\n"},
		{ID: "ps_go", Kind: library.KindRule, Name: "Go style", Order: 20, Activation: library.ActivationFiles, Scope: []string{"**/*.go"}, Body: "## Go style\n\nUse tabs.\n"},
		{ID: "ps_agent", Kind: library.KindRule, Name: "Releases", Order: 30, Activation: library.ActivationAgent, Body: "## Releases\n\nUpdate the changelog.\n"},
		{ID: "ps_gemini", Kind: library.KindRule, Name: "Gemini notes", Order: 40, Targets: library.Targets{Include: []string{adapter.GeminiCLI}}, Body: "## Gemini notes\n\nPlan first.\n"},
		{ID: "ps_skill", Kind: library.KindSkill, Name: "release-notes", Description: "Draft release notes.", Body: "Summarise the changes.\n", Files: map[string]library.File{"scripts/run.sh": {Data: []byte("#!/bin/sh\n"), Mode: 0o755}}},
		{ID: "ps_command", Kind: library.KindCommand, Name: "git:commit", Description: "Write a \"commit\" message", Body: "Write a commit message for {{args}}.\n"},
		{ID: "ps_sub", Kind: library.KindSubagent, Name: "code-reviewer", Description: "Reviews diffs.", Meta: map[string]any{"gemini_model": "example-model", "gemini_tools": []any{"read_file"}, "gemini_max_turns": 10}, Body: "Be strict.\n"},
		guard([]string{"git push --force *", "rm -rf"}, nil),
		allow("tracker"),
		{ID: "ps_hook", Kind: library.KindHook, Name: "Guard edits", Native: map[string]any{adapter.GeminiCLI: map[string]any{
			"event": "BeforeTool", "matcher": "write_file|replace", "hooks": []any{map[string]any{"type": "command", "command": "/opt/example/hooks/guard.sh", "timeout": 30000}},
		}}},
		server("ps_mcp", "tracker", library.MCPServer{
			Transport: "stdio", Command: "example-tracker", Args: []string{"--verbose"},
			Env: map[string]library.Value{"TRACKER_TOKEN": {Ref: "env:TRACKER_TOKEN"}, "MODE": {Literal: "readonly"}},
		}),
		server("ps_remote", "remote", library.MCPServer{
			Transport: "http", URL: "https://mcp.example.com/mcp",
			Headers: map[string]library.Value{"Authorization": {Literal: "Bearer ${env:REMOTE_TOKEN}"}},
		}),
	}
}

func TestDetect(t *testing.T) {
	a := registered(t)
	if a.ID() != adapter.GeminiCLI {
		t.Errorf("ID %q", a.ID())
	}
	env := testenv.Home(t, "testdata/basic/home")
	settings := filepath.Join(env.Home, ".gemini", "settings.json")
	if got := a.Detect(env); !got.Installed || !reflect.DeepEqual(got.Paths, []string{settings}) {
		t.Errorf("detection %+v", got)
	}
	if err := os.Remove(settings); err != nil {
		t.Fatal(err)
	}
	if got := a.Detect(env); got.Installed || len(got.Paths) != 0 {
		t.Errorf("a .gemini folder alone counted as installed %+v", got)
	}
	env.LookPath = func(name string) (string, error) {
		if name != "gemini" {
			t.Errorf("looked up %s", name)
		}
		return "/usr/local/bin/" + name, nil
	}
	if got := a.Detect(env); !got.Installed {
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
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	before := testenv.TreeHash(t, tg.Env.Home)
	imported := importFrom(t, tg)
	testenv.Golden(t, "testdata/golden/import-user.yaml", importYAML(t, tg, imported))
	if testenv.TreeHash(t, tg.Env.Home) != before {
		t.Error("import changed the home")
	}
}

func TestImportProjectScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.GeminiCLI, adapter.AgentsMD)
	imported := importFrom(t, tg)
	testenv.Golden(t, "testdata/golden/import-project.yaml", importYAML(t, tg, imported))
}

func TestTildeImportsUnresolved(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	memory := filepath.Join(tg.Env.Home, ".gemini", "GEMINI.md")
	want := []adapter.Unresolved{
		{File: memory, Line: "3", Target: "[style](~/notes/style.md)", Reason: "syntax"},
		{File: memory, Line: "4", Target: "[x](~/x.md)", Reason: "syntax"},
		{File: memory, Line: "5", Target: "~/tilde.md", Reason: "tilde"},
	}
	if got := importFrom(t, tg).Unresolved; !reflect.DeepEqual(got, want) {
		t.Errorf("unresolved %+v", got)
	}
}

func TestValidRelativeImportResolved(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	imported := importFrom(t, tg)
	testingRule := find(t, imported.Items, library.KindRule, "Testing")
	if testingRule.Source != filepath.Join(tg.Env.Home, ".gemini", "y.md") || testingRule.Body != "## Testing\n\nRun the tests.\n" {
		t.Errorf("imported rule %+v", testingRule)
	}
	if managedRule := find(t, imported.Items, library.KindRule, "Managed"); managedRule.Body != "## Managed\n\nKept in the section.\n" {
		t.Errorf("managed rule %q", managedRule.Body)
	}
	if preamble := find(t, imported.Items, library.KindRule, "GEMINI"); preamble.Body != "Personal defaults.\n" {
		t.Errorf("preamble %q", preamble.Body)
	}
}

func TestImportFollowsContextFileName(t *testing.T) {
	for _, value := range []string{`"CONTEXT.md"`, `["CONTEXT.md", "GEMINI.md"]`} {
		tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
		write(t, filepath.Join(tg.Env.Home, ".gemini", "settings.json"), `{"context":{"fileName":`+value+`}}`)
		write(t, filepath.Join(tg.Env.Home, ".gemini", "CONTEXT.md"), "## Custom\n\nCustom file.\n")
		imported := importFrom(t, tg)
		find(t, imported.Items, library.KindRule, "Custom")
		for _, it := range imported.Items {
			if it.Name == "Reviews" {
				t.Errorf("%s still imported GEMINI.md", value)
			}
		}
		units, _ := export(t, tg, libraryItems()[:1])
		if len(units) != 1 || units[0].Path != filepath.Join(tg.Env.Home, ".gemini", "CONTEXT.md") {
			t.Errorf("%s units %s", value, describe(tg, units))
		}
	}
}

func TestContextFileNameOutsideFolderReported(t *testing.T) {
	for _, config := range []string{
		`{"context":{"fileName":"../outside.md"}}`,
		`{"context":{"fileName":"/etc/example.md"}}`,
		`{"context":{"fileName":"settings.json"}}`,
		`{"context":{"fileName":"oauth_creds.json"}}`,
		`{"context":{"fileName":7}}`,
		`{"context":`,
	} {
		tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
		write(t, filepath.Join(tg.Env.Home, ".gemini", "settings.json"), config)
		units, unsupported := export(t, tg, libraryItems()[:1])
		if len(units) != 0 || len(unsupported) != 1 || unsupported[0].ItemID != "ps_always" {
			t.Errorf("%s units %s unsupported %+v", config, describe(tg, units), unsupported)
		}
		for _, it := range importFrom(t, tg).Items {
			if it.Kind == library.KindRule {
				t.Errorf("%s imported a rule %+v", config, it)
			}
		}
	}
}

func TestImportDanglingMemoryLinkNoted(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	memory := filepath.Join(tg.Env.Home, ".gemini", "GEMINI.md")
	if err := os.Remove(filepath.Join(tg.Env.Home, "dotfiles", "gemini", "GEMINI.md")); err != nil {
		t.Fatal(err)
	}
	imported := importFrom(t, tg)
	want := adapter.Note{Path: memory, Message: "not a readable file; rules not imported"}
	if !slices.Contains(imported.Notes, want) {
		t.Errorf("notes %+v", imported.Notes)
	}
	find(t, imported.Items, library.KindMCP, "docs")
}

func TestImportRemoteAgentWithoutKindSkipped(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	path := filepath.Join(tg.Env.Home, ".gemini", "agents", "far.md")
	write(t, path, "---\nname: far\nagent_card_url: https://agents.example.com/far\n---\n")
	imported := importFrom(t, tg)
	for _, it := range imported.Items {
		if it.Name == "far" {
			t.Errorf("imported %+v", it)
		}
	}
	if want := (adapter.Note{Path: path, Message: "remote agent; not imported"}); !slices.Contains(imported.Notes, want) {
		t.Errorf("notes %+v", imported.Notes)
	}
}

func TestSecretsScrubbedOnImport(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.GeminiCLI)
		report := importYAML(t, tg, importFrom(t, tg))
		for _, secret := range []string{"fixture-token-value", "fixture-bearer-value", "fixture-secret-value"} {
			if bytes.Contains(report, []byte(secret)) {
				t.Errorf("%s import carries %s", scope, secret)
			}
		}
	}
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	docs := find(t, importFrom(t, tg).Items, library.KindMCP, "docs")
	if docs.MCP.Env["DOCS_TOKEN"] != (library.Value{Ref: "env:DOCS_TOKEN"}) {
		t.Errorf("docs %+v", docs.MCP)
	}
}

func TestExportUserScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	items := libraryItems()
	units, unsupported := export(t, tg, items)
	testenv.Golden(t, "testdata/golden/units-user.txt", describe(tg, units))
	want := []adapter.Unsupported{
		{ItemID: "ps_go", Name: "Go style", Tool: adapter.GeminiCLI, Reason: "Gemini CLI has no scoped rules"},
		{ItemID: "ps_agent", Name: "Releases", Tool: adapter.GeminiCLI, Reason: "Gemini CLI has no scoped rules"},
	}
	if !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}
	if !reflect.DeepEqual(items, libraryItems()) {
		t.Error("export changed the items it was given")
	}
	blocked := map[string]string{}
	for _, change := range diff(t, units) {
		if change.Blocked != "" {
			blocked[relative(tg, change.Path)] = change.Blocked
		}
	}
	wantBlocked := map[string]string{
		"<home>/.agents/skills/release-notes/SKILL.md": "not created by promptstor",
		"<home>/.gemini/GEMINI.md":                     "symlinked -> ../dotfiles/gemini/GEMINI.md",
		"<home>/.gemini/commands/git/commit.toml":      "not created by promptstor",
	}
	if !maps.Equal(blocked, wantBlocked) {
		t.Errorf("blocked %v", blocked)
	}
}

func TestExportSymlinkedMemoryFileBlocked(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	units, _ := export(t, tg, libraryItems()[:1])
	before := testenv.TreeHash(t, tg.Env.Home)
	changes := diff(t, units)
	if len(changes) != 1 || !strings.HasPrefix(changes[0].Blocked, "symlinked -> ../dotfiles/gemini/GEMINI.md") {
		t.Errorf("changes %+v", changes)
	}
	if testenv.TreeHash(t, tg.Env.Home) != before {
		t.Error("the home changed")
	}
}

func TestExportProjectScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.ClaudeCode, adapter.GeminiCLI, adapter.AgentsMD)
	tg.ProjectRoot = t.TempDir()
	write(t, filepath.Join(tg.ProjectRoot, ".gemini", "settings.json"), "{\n  \"mcp\": { \"allowed\": [\"docs\"] }\n}\n")
	units, unsupported := export(t, tg, libraryItems())
	testenv.Golden(t, "testdata/golden/units-project.txt", describe(tg, units))
	want := []adapter.Unsupported{
		{ItemID: "ps_gemini", Name: "Gemini notes", Tool: adapter.GeminiCLI, Reason: agentsOnlyReason},
		{ItemID: "ps_perm", Name: "Guard", Tool: adapter.GeminiCLI, Reason: projectDenyReason, Blocking: true},
	}
	if !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}
	for _, u := range units {
		if filepath.Base(u.Path) == "GEMINI.md" || strings.Contains(u.Path, "policies") {
			t.Errorf("unit %s", u.Path)
		}
	}
	diff(t, units)
}

func TestProjectContextFileNameOwned(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.GeminiCLI, adapter.AgentsMD)
	tg.ProjectRoot = t.TempDir()
	settings := filepath.Join(tg.ProjectRoot, ".gemini", "settings.json")
	units, unsupported := export(t, tg, libraryItems()[:1])
	if len(units) != 1 || len(unsupported) != 0 {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	u := units[0]
	if u.Path != settings || u.Kind != manifest.UnitJSON || u.Key != "/context/fileName" || string(u.Content) != `["AGENTS.md","GEMINI.md"]` || !reflect.DeepEqual(u.Items, []library.ID{"ps_always"}) {
		t.Fatalf("unit %s", describe(tg, units))
	}
	write(t, settings, "{\n  // mine\n  \"context\": { \"includeDirectoryTree\": false }\n}\n")
	changes := diff(t, units)
	if len(changes) != 1 || changes[0].Op != plan.OpUpdate || !bytes.Contains(changes[0].After, []byte("// mine")) {
		t.Fatalf("changes %+v", changes)
	}
	write(t, settings, string(changes[0].After))
	units, unsupported = export(t, tg, libraryItems()[:1])
	if len(unsupported) != 0 || len(units) != 1 || diff(t, units)[0].Op != plan.OpNoop {
		t.Errorf("second export is not a noop %s %+v", describe(tg, units), unsupported)
	}
	if units, _ := export(t, tg, libraryItems()[4:5]); slices.ContainsFunc(units, func(u plan.Unit) bool { return u.Path == settings }) {
		t.Errorf("context.fileName written with no shared rules %s", describe(tg, units))
	}
}

func TestUserContextFileNameConflictBlocked(t *testing.T) {
	for _, value := range []string{`"CONTEXT.md"`, `["GEMINI.md","AGENTS.md"]`, `"AGENTS.md"`, `null`} {
		tg := target(t, "basic", adapter.ScopeProject, adapter.GeminiCLI, adapter.AgentsMD)
		settings := filepath.Join(tg.ProjectRoot, ".gemini", "settings.json")
		write(t, settings, `{"context":{"fileName":`+value+`}}`)
		units, unsupported := export(t, tg, libraryItems()[:1])
		want := []adapter.Unsupported{{ItemID: "ps_always", Name: "Reviews", Tool: adapter.GeminiCLI, Reason: userFileReason, Blocking: true}}
		if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
			t.Errorf("%s units %s unsupported %+v", value, describe(tg, units), unsupported)
		}
		note := adapter.Note{Path: settings, Message: userFileReason}
		if !slices.Contains(importFrom(t, tg).Notes, note) {
			t.Errorf("%s no note", value)
		}
	}
}

func TestPolicyFileGolden(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	tg.Env.Home = "/home/fixture"
	second := library.Item{ID: "ps_second", Kind: library.KindPermission, Name: "Keys", Portable: &library.PortablePermission{
		DenyCommands: []string{"  terraform   destroy  *"},
		DenyPaths:    []string{"**/.env", "/etc/example/**", "~/.ssh/**", "secrets/*.pem", "a/**/b?.txt"},
	}}
	units, unsupported := export(t, tg, []library.Item{guard([]string{"git push --force *", "rm -rf"}, nil), second})
	want := []adapter.Unsupported{{ItemID: "ps_second", Name: "Keys", Tool: adapter.GeminiCLI, Reason: partialPathReason, Blocking: true}}
	if !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}
	if len(units) != 1 {
		t.Fatalf("units %s", describe(tg, units))
	}
	testenv.Golden(t, "testdata/golden/promptstor.toml", units[0].Content)
	if units[0].Path != "/home/fixture/.gemini/policies/promptstor.toml" || units[0].Kind != manifest.UnitFile || !reflect.DeepEqual(units[0].Items, []library.ID{"ps_perm", "ps_second"}) {
		t.Errorf("unit %+v", units[0])
	}
}

func TestPathDenyPatternsMatchFileToolArguments(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	tg.Env.Home = "/home/fixture"
	nestedQuantifier := regexp.MustCompile(`\([^)]*[*+?{].*\)[*+?{]`)
	cases := []struct {
		glob          string
		match, others []string
	}{
		{"**/.env", []string{".env", "/work/app/.env"}, []string{"/work/app/.envrc", "/work/app/x.env"}},
		{"/etc/example/**", []string{"/etc/example/a", "/etc/example/a/b"}, []string{"/srv/etc/example/a", "/etc/examples/a"}},
		{"~/.ssh/**", []string{"~/.ssh/id", "/home/fixture/.ssh/id"}, []string{"/home/other/.ssh/id"}},
		{"secrets/*.pem", []string{"secrets/a.pem", "/work/secrets/a.pem"}, []string{"secrets/deep/a.pem", "oldsecrets/a.pem"}},
		{"a/**/b?.txt", []string{"a/b1.txt", "a/x/y/b2.txt"}, []string{"a/b.txt", "a/b/c.txt"}},
		{".env", []string{".env", "sub/.env"}, []string{"sub/not.env"}},
	}
	for _, c := range cases {
		units, unsupported := export(t, tg, []library.Item{guard(nil, []string{c.glob})})
		if len(units) != 1 || len(unsupported) != 1 || !unsupported[0].Blocking {
			t.Fatalf("%s units %s unsupported %+v", c.glob, describe(tg, units), unsupported)
		}
		var doc struct {
			Rule []struct {
				ToolName    []string `toml:"toolName"`
				ArgsPattern string   `toml:"argsPattern"`
				Decision    string   `toml:"decision"`
				Priority    int      `toml:"priority"`
			} `toml:"rule"`
		}
		if err := toml.Unmarshal(units[0].Content, &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Rule) != 1 || doc.Rule[0].Decision != "deny" || doc.Rule[0].Priority != 999 || !reflect.DeepEqual(doc.Rule[0].ToolName, []string{"read_file", "write_file", "replace"}) {
			t.Fatalf("%s rules %+v", c.glob, doc.Rule)
		}
		if nestedQuantifier.MatchString(doc.Rule[0].ArgsPattern) {
			t.Errorf("%s pattern %s would be rejected as a nested quantifier", c.glob, doc.Rule[0].ArgsPattern)
		}
		pattern := regexp.MustCompile(doc.Rule[0].ArgsPattern)
		arguments := func(path string) string {
			return "{\x00\"content\":\"x\"\x00,\x00\"file_path\":\"" + path + "\"\x00}"
		}
		for _, path := range c.match {
			if !pattern.MatchString(arguments(path)) {
				t.Errorf("%s does not deny %s with %s", c.glob, path, pattern)
			}
		}
		for _, path := range c.others {
			if pattern.MatchString(arguments(path)) {
				t.Errorf("%s denies %s with %s", c.glob, path, pattern)
			}
		}
	}
}

func TestInexpressibleDeniesBlocked(t *testing.T) {
	for _, command := range []string{"git * --force", "npm*", `rm "my file"`, "echo 'x'", "a && b", "*", "  "} {
		tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
		units, unsupported := export(t, tg, []library.Item{guard([]string{command}, nil)})
		if len(units) != 0 || len(unsupported) != 1 || !unsupported[0].Blocking {
			t.Errorf("%q units %s unsupported %+v", command, describe(tg, units), unsupported)
		}
	}
	for _, path := range []string{"", "!keep/me", "../outside/**", "a/../b", "./secrets/**", "~other/x", "a{b,c}", "a[0]", `a"b`, `a\b`, strings.Repeat("long/", 500)} {
		tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
		units, unsupported := export(t, tg, []library.Item{guard(nil, []string{path})})
		if len(units) != 0 || len(unsupported) != 1 || !unsupported[0].Blocking {
			t.Errorf("%q units %s unsupported %+v", path, describe(tg, units), unsupported)
		}
	}
}

func TestProjectDeniesBlocked(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.GeminiCLI)
	for _, item := range []library.Item{guard([]string{"rm -rf"}, nil), guard(nil, []string{"**/.env"})} {
		units, unsupported := export(t, tg, []library.Item{item})
		want := []adapter.Unsupported{{ItemID: "ps_perm", Name: "Guard", Tool: adapter.GeminiCLI, Reason: projectDenyReason, Blocking: true}}
		if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
			t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
		}
	}
}

func TestAutoSavedPolicyUntouched(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	saved := filepath.Join(tg.Env.Home, ".gemini", "policies", "auto-saved.toml")
	before, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	units, _ := export(t, tg, libraryItems())
	changes := diff(t, units)
	for _, change := range changes {
		if change.Path == saved {
			t.Errorf("planned change %+v", change)
		}
	}
	if after, err := os.ReadFile(saved); err != nil || !bytes.Equal(after, before) {
		t.Errorf("auto-saved.toml changed, %v", err)
	}
	imported := importFrom(t, tg)
	for _, it := range imported.Items {
		if it.Source == saved {
			t.Errorf("imported %+v", it)
		}
	}
	if want := (adapter.Note{Path: saved, Message: "written by Gemini CLI; not read"}); !slices.Contains(imported.Notes, want) {
		t.Errorf("notes %+v", imported.Notes)
	}
}

func TestAllowedServersExtendOnlyAnExistingList(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	units, unsupported := export(t, tg, []library.Item{allow("tracker")})
	if len(units) != 1 || len(unsupported) != 0 || units[0].Kind != manifest.UnitJSONElement || units[0].Key != "/mcp/allowed@=tracker" || string(units[0].Content) != `"tracker"` {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	changes := diff(t, units)
	list, found, err := structedit.JSONGet(changes[0].After, "/mcp/allowed")
	if err != nil || !found || !bytes.Contains(list, []byte(`"docs"`)) || !bytes.Contains(list, []byte(`"tracker"`)) {
		t.Errorf("allowed %s %v", list, err)
	}
	if bytes.Contains(changes[0].After, []byte(`"tools": {"allowed"`)) || !bytes.Contains(changes[0].After, []byte("/* servers the user trusts */")) {
		t.Errorf("settings %s", changes[0].After)
	}
	for _, config := range []string{"{}", `{"mcp":{}}`, `{"mcp":{"allowed":[]}}`} {
		write(t, filepath.Join(tg.Env.Home, ".gemini", "settings.json"), config)
		units, unsupported := export(t, tg, []library.Item{allow("tracker")})
		want := []adapter.Unsupported{{ItemID: "ps_allow", Name: "Servers", Tool: adapter.GeminiCLI, Reason: noAllowListReason}}
		if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
			t.Errorf("%s units %s unsupported %+v", config, describe(tg, units), unsupported)
		}
	}
}

func TestToolsExcludeNeverWritten(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.GeminiCLI)
		units, _ := export(t, tg, libraryItems())
		for _, u := range units {
			if strings.HasPrefix(u.Key, "/tools") {
				t.Errorf("unit %s", u.Key)
			}
		}
	}
}

func TestCommandTOMLGolden(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	body := "Quote \"\"\"three\"\"\" and C:\\temp.\nTab\there, bell\a, ends with \""
	items := []library.Item{{ID: "ps_cmd", Kind: library.KindCommand, Name: "git:Commit All", Description: "Say \"hi\"\nand more \\ done", Meta: map[string]any{"argument-hint": "[scope]"}, Body: body}}
	units, unsupported := export(t, tg, items)
	want := []adapter.Unsupported{{ItemID: "ps_cmd", Name: "git:Commit All", Tool: adapter.GeminiCLI, Reason: "written without argument-hint, which Gemini CLI command files do not take"}}
	if !reflect.DeepEqual(unsupported, want) || len(units) != 1 {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	if units[0].Path != filepath.Join(tg.Env.Home, ".gemini", "commands", "git", "commit-all.toml") || units[0].Kind != manifest.UnitFile {
		t.Errorf("unit %+v", units[0])
	}
	testenv.Golden(t, "testdata/golden/command.toml", units[0].Content)
	var decoded map[string]any
	if err := toml.Unmarshal(units[0].Content, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["prompt"] != body || decoded["description"] != "Say \"hi\"\nand more \\ done" || len(decoded) != 2 {
		t.Errorf("decoded %q", decoded)
	}
}

func TestSubagentFrontmatter(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	items := []library.Item{{ID: "ps_sub", Kind: library.KindSubagent, Name: "Code Reader", Description: "Reads.", Meta: map[string]any{"model": "sonnet", "tools": "Read", "gemini_max_turns": "ten", "gemini_temperature": 0.2}, Body: "Read.\n"}}
	units, unsupported := export(t, tg, items)
	want := []adapter.Unsupported{{ItemID: "ps_sub", Name: "Code Reader", Tool: adapter.GeminiCLI, Reason: "written without gemini_max_turns, model, tools, which Gemini CLI agent files do not take"}}
	if len(units) != 1 || !reflect.DeepEqual(unsupported, want) {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	if want := "---\nname: code-reader\ndescription: Reads.\ntemperature: 0.2\n---\n"; !strings.HasPrefix(string(units[0].Content), want) {
		t.Errorf("content %s", units[0].Content)
	}
}

func TestMemoryWriteLabelled(t *testing.T) {
	labeller, ok := registered(t).(interface {
		HandEditDetail(adapter.Target, string, manifest.UnitKind) string
	})
	if !ok {
		t.Fatal("the adapter does not label hand edits")
	}
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	memory := filepath.Join(tg.Env.Home, ".gemini", "GEMINI.md")
	if got := labeller.HandEditDetail(tg, memory, manifest.UnitSection); got != "agent memory write" {
		t.Errorf("detail %q", got)
	}
	for _, other := range []struct {
		path string
		kind manifest.UnitKind
	}{
		{memory, manifest.UnitFile},
		{filepath.Join(tg.Env.Home, ".gemini", "settings.json"), manifest.UnitSection},
	} {
		if got := labeller.HandEditDetail(tg, other.path, other.kind); got != "" {
			t.Errorf("%s %s detail %q", other.kind, other.path, got)
		}
	}
	write(t, filepath.Join(tg.Env.Home, ".gemini", "settings.json"), `{"context":{"fileName":"CONTEXT.md"}}`)
	if got := labeller.HandEditDetail(tg, filepath.Join(tg.Env.Home, ".gemini", "CONTEXT.md"), manifest.UnitSection); got != "agent memory write" {
		t.Errorf("renamed memory file detail %q", got)
	}
}

func TestNativeEnvRefOnExport(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.GeminiCLI)
		mcp := library.MCPServer{
			Transport: "http", URL: "https://mcp.example.com/mcp?region=${env:REGION}",
			Headers: map[string]library.Value{"Authorization": {Literal: "Bearer ${env:REMOTE_TOKEN}"}, "X-Key": {Ref: "env:REMOTE_KEY"}},
		}
		prepared, _, needsSetup, err := secrets.Prepare(mcp, registered(t).Capabilities().EnvRef, nil)
		if err != nil || len(needsSetup) != 0 {
			t.Fatalf("prepare %v %v", err, needsSetup)
		}
		for _, candidate := range []library.MCPServer{mcp, prepared} {
			units, unsupported := export(t, tg, []library.Item{server("ps_fresh", "fresh", candidate)})
			if len(units) != 1 || len(unsupported) != 0 {
				t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
			}
			want := `{"url":"https://mcp.example.com/mcp?region=${REGION}","type":"http","headers":{"Authorization":"Bearer ${REMOTE_TOKEN}","X-Key":"${REMOTE_KEY}"}}`
			if string(units[0].Content) != want || units[0].Key != "/mcpServers/fresh" {
				t.Errorf("%s unit %s", scope, describe(tg, units))
			}
		}
	}
}

func TestExportKeepsUnknownServerSettings(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	docs := server("ps_docs", "docs", library.MCPServer{Transport: "stdio", Command: "example-docs"})
	units, unsupported := export(t, tg, []library.Item{docs})
	want := []adapter.Unsupported{{ItemID: "ps_docs", Name: "docs", Tool: adapter.GeminiCLI, Reason: "mcpServers/docs has timeout, trust, which PromptStor would drop; set the server up by hand"}}
	if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestUnsupportedReported(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	items := []library.Item{
		{ID: "ps_agent", Kind: library.KindRule, Name: "Releases", Activation: library.ActivationAgent, Body: "## Releases\n"},
		{ID: "ps_skill", Kind: library.KindSkill, Name: "../escape", Body: "x\n"},
		{ID: "ps_cred", Kind: library.KindSkill, Name: "tokens", Body: "x\n", Files: map[string]library.File{"scripts/token.json": {Data: []byte("{}")}}},
		{ID: "ps_cmd", Kind: library.KindCommand, Name: "mute", Body: "  "},
		{ID: "ps_cmdname", Kind: library.KindCommand, Name: "!!", Body: "x\n"},
		{ID: "ps_cmdtoken", Kind: library.KindCommand, Name: "token", Body: "x\n"},
		{ID: "ps_sub", Kind: library.KindSubagent, Name: "nameless", Body: "x\n"},
		{ID: "ps_native", Kind: library.KindPermission, Name: "Native", Native: map[string]any{adapter.GeminiCLI: map[string]any{}}},
		{ID: "ps_foreign", Kind: library.KindPermission, Name: "Claude only", Native: map[string]any{adapter.ClaudeCode: map[string]any{}}},
		{ID: "ps_hook", Kind: library.KindHook, Name: "Other tool", Native: map[string]any{adapter.ClaudeCode: map[string]any{}}},
		{ID: "ps_event", Kind: library.KindHook, Name: "Bad event", Native: map[string]any{adapter.GeminiCLI: map[string]any{"event": "PreToolUse", "hooks": []any{map[string]any{"type": "command", "command": "x"}}}}},
		{ID: "ps_nocommand", Kind: library.KindHook, Name: "No command", Native: map[string]any{adapter.GeminiCLI: map[string]any{"event": "BeforeTool"}}},
		{ID: "ps_nomcp", Kind: library.KindMCP, Name: "empty"},
		server("ps_ws", "ws", library.MCPServer{Transport: "ws", URL: "wss://example.com"}),
		server("ps_keychain", "kc", library.MCPServer{Transport: "stdio", Command: "x", Env: map[string]library.Value{"API_KEY": {Ref: "keychain:example"}}}),
		server("ps_both", "both", library.MCPServer{Transport: "http", URL: "https://example.com", Command: "x"}),
	}
	units, unsupported := export(t, tg, items)
	if len(units) != 0 {
		t.Errorf("units %s", describe(tg, units))
	}
	var got []string
	for _, u := range unsupported {
		if u.Tool != adapter.GeminiCLI || u.Blocking {
			t.Errorf("unsupported %+v", u)
		}
		got = append(got, string(u.ItemID)+": "+u.Reason)
	}
	testenv.Golden(t, "testdata/golden/unsupported.txt", []byte(strings.Join(got, "\n")+"\n"))
}

func TestBackupSet(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.GeminiCLI)
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
			tg := target(t, "roundtrip", scope, adapter.GeminiCLI, adapter.AgentsMD)
			homeBefore, projectBefore := testenv.TreeHash(t, tg.Env.Home), testenv.TreeHash(t, tg.ProjectRoot)
			imported := importFrom(t, tg)
			if len(imported.Items) == 0 || len(imported.Notes) != 0 || len(imported.Unresolved) != 0 {
				t.Fatalf("imported %s", importYAML(t, tg, imported))
			}
			items := imported.Items
			if scope == adapter.ScopeProject {
				items = append(items, libraryItems()[0])
			}
			units, unsupported := export(t, tg, items)
			if len(unsupported) != 0 {
				t.Errorf("unsupported %+v", unsupported)
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
			if testenv.TreeHash(t, tg.Env.Home) != homeBefore || testenv.TreeHash(t, tg.ProjectRoot) != projectBefore {
				t.Error("the fixture changed")
			}
		})
	}
}

func TestHomeOverrideNoted(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.GeminiCLI)
	tg.Env.Getenv = func(name string) string {
		if name == "GEMINI_CLI_HOME" {
			return "/elsewhere"
		}
		return ""
	}
	want := adapter.Note{Path: filepath.Join(tg.Env.Home, ".gemini"), Message: "GEMINI_CLI_HOME is set; promptstor reads only ~/.gemini"}
	if notes := importFrom(t, tg).Notes; !slices.Contains(notes, want) {
		t.Errorf("notes %+v", notes)
	}
}
