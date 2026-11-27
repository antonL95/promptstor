package codex_test

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

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"

	"github.com/antonL95/promptstor/internal/adapter"
	_ "github.com/antonL95/promptstor/internal/adapter/codex"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/secrets"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	homeMarker    = "<home>"
	projectMarker = "<project>"
	overrideNote  = "overrides AGENTS.md; PromptStor rules are not loaded"
	profileReason = "Codex path denies need default_permissions, which PromptStor cannot set"

	unconfirmedReason = "Codex profile denies are not confirmed to cover apply_patch and built-in file tools"
	bareProfile       = "[permissions.promptstor]\nextends = \":workspace\""
)

func registered(t *testing.T) adapter.Adapter {
	t.Helper()
	a, ok := adapter.Get(adapter.Codex)
	if !ok {
		t.Fatal("codex is not registered")
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
	changes, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.Codex}, plan.Options{})
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
	Meta        map[string]any              `yaml:"meta,omitempty"`
	Files       []string                    `yaml:"files,omitempty"`
	Native      map[string]any              `yaml:"native,omitempty"`
	Portable    *library.PortablePermission `yaml:"portable,omitempty"`
	MCP         *library.MCPServer          `yaml:"mcp,omitempty"`
	Body        string                      `yaml:"body,omitempty"`
}

type reportView struct {
	Items   []itemView        `yaml:"items"`
	Foreign []adapter.Foreign `yaml:"foreign,omitempty"`
	Grants  []adapter.Grant   `yaml:"grants,omitempty"`
	Notes   []adapter.Note    `yaml:"notes,omitempty"`
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

func server(id, name string, mcp library.MCPServer) library.Item {
	return library.Item{ID: library.ID(id), Kind: library.KindMCP, Name: name, MCP: &mcp}
}

func libraryItems() []library.Item {
	return []library.Item{
		{ID: "ps_always", Kind: library.KindRule, Name: "Reviews", Order: 10, Body: "## Reviews\n\nBe critical.\n"},
		{ID: "ps_go", Kind: library.KindRule, Name: "Go style", Order: 20, Activation: library.ActivationFiles, Scope: []string{"**/*.go"}, Body: "## Go style\n\nUse tabs.\n"},
		{ID: "ps_agent", Kind: library.KindRule, Name: "Releases", Order: 30, Activation: library.ActivationAgent, Body: "## Releases\n\nUpdate the changelog.\n"},
		{ID: "ps_codex", Kind: library.KindRule, Name: "Codex notes", Order: 40, Targets: library.Targets{Include: []string{adapter.Codex}}, Body: "## Codex notes\n\nPlan first.\n"},
		{ID: "ps_skill", Kind: library.KindSkill, Name: "release-notes", Description: "Draft release notes.", Body: "Summarise the changes.\n", Files: map[string]library.File{"scripts/run.sh": {Data: []byte("#!/bin/sh\n"), Mode: 0o755}}},
		{ID: "ps_command", Kind: library.KindCommand, Name: "git:commit", Description: "Write a commit message", Meta: map[string]any{"argument-hint": "[scope]"}, Body: "Write a commit message.\n"},
		{ID: "ps_sub", Kind: library.KindSubagent, Name: "code-reviewer", Description: "Reviews diffs.", Meta: map[string]any{"codex_model": "example-model", "model_reasoning_effort": "high"}, Body: "Be \"\"\"strict\"\"\" about C:\\temp.\n"},
		guard([]string{"git push --force *", "rm -rf"}, nil),
		{ID: "ps_hook", Kind: library.KindHook, Name: "Guard edits", Native: map[string]any{adapter.Codex: map[string]any{
			"event": "PreToolUse", "matcher": "apply_patch|Bash", "hooks": []any{map[string]any{"type": "command", "command": "/opt/example/hooks/guard.sh", "timeout": 30}},
		}}},
		server("ps_mcp", "docs", library.MCPServer{
			Transport: "stdio", Command: "example-docs", Args: []string{"--verbose"},
			Env: map[string]library.Value{"DOCS_TOKEN": {Ref: "env:DOCS_TOKEN"}, "MODE": {Literal: "read \"only\""}},
		}),
		server("ps_remote", "remote.api", library.MCPServer{
			Transport: "http", URL: "https://mcp.example.com/mcp",
			Headers: map[string]library.Value{"Authorization": {Literal: "Bearer ${env:REMOTE_TOKEN}"}, "X-Static": {Literal: "v1"}, "X-Team": {Ref: "env:TEAM_NAME"}},
		}),
	}
}

func TestDetect(t *testing.T) {
	a := registered(t)
	if a.ID() != adapter.Codex {
		t.Errorf("ID %q", a.ID())
	}
	env := testenv.Home(t, "testdata/basic/home")
	got := a.Detect(env)
	if !got.Installed || !reflect.DeepEqual(got.Paths, []string{filepath.Join(env.Home, ".codex")}) {
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

func TestCodexHomeOverride(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	moved := t.TempDir()
	write(t, filepath.Join(moved, "AGENTS.md"), "## Moved\n\nElsewhere.\n")
	tg.Env.Getenv = func(name string) string {
		if name == "CODEX_HOME" {
			return moved
		}
		return ""
	}
	if got := registered(t).Detect(tg.Env); !reflect.DeepEqual(got.Paths, []string{moved}) {
		t.Errorf("detection %+v", got)
	}
	find(t, importFrom(t, tg).Items, library.KindRule, "Moved")
	units, _ := export(t, tg, libraryItems())
	for _, u := range units {
		inMoved := strings.HasPrefix(u.Path, moved+string(filepath.Separator))
		inAgents := strings.HasPrefix(u.Path, filepath.Join(tg.Env.Home, ".agents")+string(filepath.Separator))
		if !inMoved && !inAgents {
			t.Errorf("unit outside the codex home %s", u.Path)
		}
		if inMoved && u.Root != filepath.Dir(moved) {
			t.Errorf("unit root %s", u.Root)
		}
	}
	diff(t, units)
	for _, entry := range registered(t).BackupSet(tg) {
		if strings.HasPrefix(entry.Path, filepath.Join(tg.Env.Home, ".codex")) {
			t.Errorf("backup entry in the default home %s", entry.Path)
		}
	}
	tg.Env.Getenv = func(string) string { return "relative/dir" }
	if got := registered(t).Detect(tg.Env); !reflect.DeepEqual(got.Paths, []string{filepath.Join(tg.Env.Home, ".codex")}) {
		t.Errorf("relative CODEX_HOME %+v", got)
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
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	before := testenv.TreeHash(t, tg.Env.Home)
	imported := importFrom(t, tg)
	testenv.Golden(t, "testdata/golden/import-user.yaml", importYAML(t, tg, imported))
	if testenv.TreeHash(t, tg.Env.Home) != before {
		t.Error("import changed the home")
	}
}

func TestImportProjectScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.Codex, adapter.AgentsMD)
	imported := importFrom(t, tg)
	testenv.Golden(t, "testdata/golden/import-project.yaml", importYAML(t, tg, imported))
}

func TestImportDefaultRulesAsGrants(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	path := filepath.Join(tg.Env.Home, ".codex", "rules", "default.rules")
	allow := `prefix_rule(pattern=["make", "test"], decision="allow")`
	network := `network_rule(host="docs.example.com", protocol="https", decision="allow")`
	want := []adapter.Grant{
		{Tool: adapter.Codex, Path: path, Key: allow, Value: allow},
		{Tool: adapter.Codex, Path: path, Key: network, Value: network},
	}
	imported := importFrom(t, tg)
	if !reflect.DeepEqual(imported.Grants, want) {
		t.Errorf("grants %+v", imported.Grants)
	}
	for _, it := range imported.Items {
		if it.Kind == library.KindPermission {
			t.Errorf("rules file imported as an item %+v", it)
		}
	}
}

func TestImportPromptsNoted(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	imported := importFrom(t, tg)
	triage := find(t, imported.Items, library.KindCommand, "triage")
	path := filepath.Join(tg.Env.Home, ".codex", "prompts", "triage.md")
	if triage.Source != path || triage.Body != "Triage issue $1.\n" || triage.Description != "Triage an issue" {
		t.Errorf("triage %+v", triage)
	}
	if want := (adapter.Note{Path: path, Message: "no longer loaded by Codex"}); !slices.Contains(imported.Notes, want) {
		t.Errorf("notes %+v", imported.Notes)
	}
}

func TestImportGeneratedLegacySkillAsCommand(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	imported := importFrom(t, tg)
	ship := find(t, imported.Items, library.KindCommand, "ship")
	if ship.Body != "Ship it.\n" || ship.Description != "Ship the branch" {
		t.Errorf("ship %+v", ship)
	}
	find(t, imported.Items, library.KindSkill, "old-skill")
	for _, it := range imported.Items {
		if it.Name == "skill-creator" {
			t.Errorf("system skill imported %+v", it)
		}
	}
}

func TestOverrideFileNoted(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.Codex)
		dir := filepath.Join(tg.Env.Home, ".codex")
		if scope == adapter.ScopeProject {
			dir = tg.ProjectRoot
		}
		path := filepath.Join(dir, "AGENTS.override.md")
		want := adapter.Note{Path: path, Message: overrideNote}
		if slices.Contains(importFrom(t, tg).Notes, want) {
			t.Errorf("%s note without the file", scope)
		}
		write(t, path, "")
		if slices.Contains(importFrom(t, tg).Notes, want) {
			t.Errorf("%s note for an empty file, which Codex skips", scope)
		}
		write(t, path, "## Override\n")
		imported := importFrom(t, tg)
		if !slices.Contains(imported.Notes, want) {
			t.Errorf("%s notes %+v", scope, imported.Notes)
		}
		for _, it := range imported.Items {
			if it.Name == "Override" {
				t.Errorf("override imported %+v", it)
			}
		}
	}
}

func TestImportToleratesOddFolders(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	agents := filepath.Join(tg.Env.Home, ".codex", "agents")
	if err := os.RemoveAll(agents); err != nil {
		t.Fatal(err)
	}
	write(t, agents, "not a folder")
	shared := filepath.Join(tg.Env.Home, ".agents", "skills")
	if err := os.Rename(shared, filepath.Join(tg.Env.Home, "kept-skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "kept-skills"), shared); err != nil {
		t.Fatal(err)
	}
	find(t, importFrom(t, tg).Items, library.KindSkill, "release-notes")
	if err := os.RemoveAll(filepath.Join(tg.Env.Home, "kept-skills")); err != nil {
		t.Fatal(err)
	}
	want := adapter.Note{Path: shared, Message: "symlinked -> ../kept-skills; not imported"}
	if imported := importFrom(t, tg); !slices.Contains(imported.Notes, want) {
		t.Errorf("notes %+v", imported.Notes)
	}
}

func TestSecretsScrubbedOnImport(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	imported := importFrom(t, tg)
	report := importYAML(t, tg, imported)
	for _, secret := range []string{"fixture-token-value", "fixture-bearer-value"} {
		if bytes.Contains(report, []byte(secret)) {
			t.Errorf("import carries %s", secret)
		}
	}
	docs := find(t, imported.Items, library.KindMCP, "docs")
	if docs.MCP.Env["DOCS_TOKEN"] != (library.Value{Ref: "needs-setup:DOCS_TOKEN"}) {
		t.Errorf("docs %+v", docs.MCP)
	}
	tracker := find(t, imported.Items, library.KindMCP, "tracker")
	if tracker.MCP.Env["TRACKER_TOKEN"] != (library.Value{Ref: "env:TRACKER_TOKEN"}) {
		t.Errorf("tracker %+v", tracker.MCP)
	}
}

func TestExportUserScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	items := libraryItems()
	units, unsupported := export(t, tg, items)
	testenv.Golden(t, "testdata/golden/units-user.txt", describe(tg, units))
	want := []adapter.Unsupported{
		{ItemID: "ps_go", Name: "Go style", Tool: adapter.Codex, Reason: "Codex has no scoped rules"},
		{ItemID: "ps_agent", Name: "Releases", Tool: adapter.Codex, Reason: "Codex has no scoped rules"},
	}
	if !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}
	if !reflect.DeepEqual(items, libraryItems()) {
		t.Error("export changed the items it was given")
	}
	diff(t, units)
}

func TestExportProjectScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.ClaudeCode, adapter.Codex, adapter.AgentsMD)
	units, unsupported := export(t, tg, libraryItems())
	testenv.Golden(t, "testdata/golden/units-project.txt", describe(tg, units))
	const agentsOnly = "Codex reads only AGENTS.md in a project"
	want := []adapter.Unsupported{
		{ItemID: "ps_codex", Name: "Codex notes", Tool: adapter.Codex, Reason: agentsOnly},
		{ItemID: "ps_command", Name: "git:commit", Tool: adapter.Codex, Reason: "Codex has no project commands"},
	}
	if !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}
	diff(t, units)
}

func TestCommandsExportedAsSkills(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	units, unsupported := export(t, tg, libraryItems()[5:6])
	if len(units) != 1 || len(unsupported) != 0 {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	want := filepath.Join(tg.Env.Home, ".codex", "skills", "git-commit", "SKILL.md")
	if units[0].Path != want || units[0].Kind != manifest.UnitFile || units[0].Root != tg.Env.Home {
		t.Errorf("unit %+v", units[0])
	}
	content := string(units[0].Content)
	if !strings.HasPrefix(content, "---\nname: git-commit\ndescription: Write a commit message\n") {
		t.Errorf("frontmatter %q", content)
	}
	if strings.Contains(content, "disable-model-invocation") {
		t.Errorf("a field Codex does not read was written %q", content)
	}
	project := tg
	project.Scope = adapter.ScopeProject
	units, unsupported = export(t, project, libraryItems()[5:6])
	if len(units) != 0 || len(unsupported) != 1 || unsupported[0].Blocking {
		t.Errorf("project units %s unsupported %+v", describe(project, units), unsupported)
	}
}

func TestExportRulesFileGolden(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	second := library.Item{ID: "ps_second", Kind: library.KindPermission, Name: "Quote \"me\"", Portable: &library.PortablePermission{DenyCommands: []string{"  terraform   destroy  *"}}}
	units, unsupported := export(t, tg, []library.Item{guard([]string{"git push --force *", "rm -rf"}, nil), second})
	if len(units) != 1 || len(unsupported) != 0 {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	testenv.Golden(t, "testdata/golden/promptstor.rules", units[0].Content)
	if units[0].Path != filepath.Join(tg.Env.Home, ".codex", "rules", "promptstor.rules") || units[0].Kind != manifest.UnitFile {
		t.Errorf("unit %+v", units[0])
	}
	if !reflect.DeepEqual(units[0].Items, []library.ID{"ps_perm", "ps_second"}) {
		t.Errorf("items %v", units[0].Items)
	}
}

func TestInexpressibleDenyCommandsBlocked(t *testing.T) {
	for _, command := range []string{"git * --force", "npm*", `rm "my file"`, "echo 'x'", "*", "  "} {
		tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
		units, unsupported := export(t, tg, []library.Item{guard([]string{command}, nil)})
		if len(units) != 0 || len(unsupported) != 1 || !unsupported[0].Blocking {
			t.Errorf("%q units %s unsupported %+v", command, describe(tg, units), unsupported)
		}
	}
}

func profileTarget(t *testing.T, config string) adapter.Target {
	t.Helper()
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	write(t, filepath.Join(tg.Env.Home, ".codex", "config.toml"), config)
	return tg
}

func TestPathDenyProfileWhenSandboxUnset(t *testing.T) {
	tg := profileTarget(t, "default_permissions = \"promptstor\"\n")
	items := []library.Item{guard(nil, []string{"**/.env", "/etc/example/**", "~/.ssh/**", "secrets/key.pem"})}
	units, unsupported := export(t, tg, items)
	if len(units) != 1 || len(unsupported) != 1 {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	testenv.Golden(t, "testdata/golden/permissions-profile.toml", units[0].Content)
	if units[0].Kind != manifest.UnitTOMLTable || units[0].Key != "permissions.promptstor" {
		t.Errorf("unit %+v", units[0])
	}
	changes := diff(t, units)
	if len(changes) != 1 || changes[0].Blocked != "" {
		t.Fatalf("changes %+v", changes)
	}
	var decoded map[string]any
	if err := toml.Unmarshal(changes[0].After, &decoded); err != nil {
		t.Fatal(err)
	}
	write(t, changes[0].Path, string(changes[0].After))
	units, unsupported = export(t, tg, items)
	if len(unsupported) != 1 || diff(t, units)[0].Op != plan.OpNoop {
		t.Errorf("second export is not a noop, unsupported %+v", unsupported)
	}
}

func TestPathDenyBlockedWhenSandboxSet(t *testing.T) {
	for name, config := range map[string]string{
		"sandbox mode set":      "default_permissions = \"promptstor\"\nsandbox_mode = \"read-only\"\n",
		"no default":            "model = \"example-model\"\n",
		"other default":         "default_permissions = \"mine\"\n",
		"default only in table": "[profile]\ndefault_permissions = \"promptstor\"\n",
		"invalid config":        "default_permissions = \n",
	} {
		t.Run(name, func(t *testing.T) {
			tg := profileTarget(t, config)
			units, unsupported := export(t, tg, []library.Item{guard([]string{"rm -rf"}, []string{"**/.env"})})
			want := []adapter.Unsupported{{ItemID: "ps_perm", Name: "Guard", Tool: adapter.Codex, Reason: profileReason, Blocking: true}}
			if !reflect.DeepEqual(unsupported, want) {
				t.Errorf("unsupported %+v", unsupported)
			}
			if len(units) != 1 || filepath.Base(units[0].Path) != "promptstor.rules" {
				t.Errorf("units %s", describe(tg, units))
			}
		})
	}
}

func TestProjectPathDenyBlockedByUserSandboxMode(t *testing.T) {
	tg := profileTarget(t, "sandbox_mode = \"read-only\"\n")
	tg.Scope = adapter.ScopeProject
	write(t, filepath.Join(tg.ProjectRoot, ".codex", "config.toml"), "default_permissions = \"promptstor\"\n")
	items := []library.Item{guard(nil, []string{"**/.env"})}
	units, unsupported := export(t, tg, items)
	if len(units) != 0 || len(unsupported) != 1 || !unsupported[0].Blocking {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	write(t, filepath.Join(tg.Env.Home, ".codex", "config.toml"), "model = \"example-model\"\n")
	units, unsupported = export(t, tg, items)
	if len(units) != 1 || len(unsupported) != 1 || unsupported[0].Reason != unconfirmedReason || units[0].Path != filepath.Join(tg.ProjectRoot, ".codex", "config.toml") {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestPathDenyKeepsUserProfileSettings(t *testing.T) {
	for _, profile := range []string{
		"[permissions.promptstor]\nextends = \":workspace\"\n\n[permissions.promptstor.network]\nenabled = true\n",
		"[permissions.promptstor]\nextends = \":read-only\"\n",
		"[permissions.promptstor.filesystem]\n\"~/Documents\" = \"read\"\n",
	} {
		tg := profileTarget(t, "default_permissions = \"promptstor\"\n\n"+profile)
		units, unsupported := export(t, tg, []library.Item{guard(nil, []string{"**/.env"})})
		if len(units) != 0 || len(unsupported) != 1 || !unsupported[0].Blocking {
			t.Errorf("%q units %s unsupported %+v", profile, describe(tg, units), unsupported)
		}
	}
}

func TestInexpressibleDenyPathsBlocked(t *testing.T) {
	for _, path := range []string{".env", "./secrets/**", "../outside/**", ":root", "a/../b", "!keep/me"} {
		tg := profileTarget(t, "default_permissions = \"promptstor\"\n")
		units, unsupported := export(t, tg, []library.Item{guard(nil, []string{path})})
		if len(units) != 1 || string(units[0].Content) != bareProfile || len(unsupported) != 1 || !unsupported[0].Blocking {
			t.Errorf("%q units %s unsupported %+v", path, describe(tg, units), unsupported)
		}
	}
}

func TestPathDenyAlwaysNeedsAck(t *testing.T) {
	second := library.Item{ID: "ps_second", Kind: library.KindPermission, Name: "Keys", Portable: &library.PortablePermission{DenyPaths: []string{"~/.ssh/**"}}}
	items := []library.Item{guard([]string{"rm -rf"}, []string{"**/.env"}), second}
	blocking := func(reason string) []adapter.Unsupported {
		return []adapter.Unsupported{
			{ItemID: "ps_perm", Name: "Guard", Tool: adapter.Codex, Reason: reason, Blocking: true},
			{ItemID: "ps_second", Name: "Keys", Tool: adapter.Codex, Reason: reason, Blocking: true},
		}
	}
	profileUnits := func(units []plan.Unit) []plan.Unit {
		return slices.DeleteFunc(slices.Clone(units), func(u plan.Unit) bool { return u.Key != "permissions.promptstor" })
	}

	tg := profileTarget(t, "default_permissions = \"promptstor\"\n")
	units, unsupported := export(t, tg, items)
	if !reflect.DeepEqual(unsupported, blocking(unconfirmedReason)) {
		t.Errorf("unsupported %+v", unsupported)
	}
	profile := profileUnits(units)
	if len(profile) != 1 || !reflect.DeepEqual(profile[0].Items, []library.ID{"ps_perm", "ps_second"}) {
		t.Fatalf("units %s", describe(tg, units))
	}
	for _, path := range []string{"**/.env", "~/.ssh/**"} {
		if !bytes.Contains(profile[0].Content, []byte(`"`+path+`" = "deny"`)) {
			t.Errorf("%s not denied\n%s", path, profile[0].Content)
		}
	}

	tg = profileTarget(t, "model = \"example-model\"\n")
	units, unsupported = export(t, tg, items)
	if !reflect.DeepEqual(unsupported, blocking(profileReason)) {
		t.Errorf("unsupported %+v", unsupported)
	}
	if len(profileUnits(units)) != 0 {
		t.Errorf("units %s", describe(tg, units))
	}
}

func TestProfileKeptWhenNoDenyPathsRemain(t *testing.T) {
	const previous = "default_permissions = \"promptstor\"\n\n[permissions.promptstor]\nextends = \":workspace\"\n\n[permissions.promptstor.filesystem.\":workspace_roots\"]\n\"**/.env\" = \"deny\"\n"
	for name, items := range map[string][]library.Item{
		"no items":      nil,
		"commands only": {guard([]string{"rm -rf"}, nil)},
	} {
		t.Run(name, func(t *testing.T) {
			tg := profileTarget(t, previous)
			units, unsupported := export(t, tg, items)
			if len(unsupported) != 0 {
				t.Errorf("unsupported %+v", unsupported)
			}
			var profile []plan.Unit
			for _, u := range units {
				if u.Key == "permissions.promptstor" {
					profile = append(profile, u)
				}
			}
			if len(profile) != 1 || string(profile[0].Content) != bareProfile || len(profile[0].Items) != 0 {
				t.Fatalf("units %s", describe(tg, units))
			}
			changes := diff(t, profile)
			if len(changes) != 1 || changes[0].Blocked != "" {
				t.Fatalf("changes %+v", changes)
			}
			if want := "default_permissions = \"promptstor\"\n\n" + bareProfile + "\n"; string(changes[0].After) != want {
				t.Errorf("config %q", changes[0].After)
			}
		})
	}
	for _, config := range []string{
		"model = \"example-model\"\n",
		"default_permissions = \"promptstor\"\nsandbox_mode = \"read-only\"\n",
		"default_permissions = \"promptstor\"\n\n[permissions.promptstor.network]\nenabled = true\n",
	} {
		tg := profileTarget(t, config)
		if units, unsupported := export(t, tg, nil); len(units) != 0 || len(unsupported) != 0 {
			t.Errorf("%q units %s unsupported %+v", config, describe(tg, units), unsupported)
		}
	}
}

func TestExportKeepsForeignTablesByteIdentical(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	path := filepath.Join(tg.Env.Home, ".codex", "config.toml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	units, _ := export(t, tg, libraryItems())
	var after []byte
	for _, change := range diff(t, units) {
		if change.Path == path {
			if change.Blocked != "" {
				t.Fatalf("blocked %s", change.Blocked)
			}
			after = change.After
		}
	}
	head, rest, _ := bytes.Cut(before, []byte("[mcp_servers.docs]"))
	_, afterDocs, _ := bytes.Cut(rest, []byte("[mcp_servers.tracker]"))
	if !bytes.HasPrefix(after, head) {
		t.Errorf("text before the owned tables changed\n%s", after)
	}
	if !bytes.Contains(after, afterDocs) {
		t.Errorf("tables after the owned one changed\n%s", after)
	}
	for _, table := range []string{"[projects.", "[hooks.state.", "[[skills.config]]", "[notice]", "[tui]", "[plugins.", "[marketplaces."} {
		if !bytes.Contains(after, []byte(table)) {
			t.Errorf("%s lost", table)
		}
	}
	for _, u := range units {
		if u.Path == path && !strings.HasPrefix(u.Key, "mcp_servers.") {
			t.Errorf("unit key %s", u.Key)
		}
	}
}

func TestExportKeepsUnknownServerSettings(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	tracker := server("ps_tracker", "tracker", library.MCPServer{Transport: "stdio", Command: "example-tracker"})
	units, unsupported := export(t, tg, []library.Item{tracker})
	want := []adapter.Unsupported{{ItemID: "ps_tracker", Name: "tracker", Tool: adapter.Codex, Reason: "[mcp_servers.tracker] has cwd, startup_timeout_sec, which PromptStor would drop; set the server up by hand"}}
	if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestExportKeepsDisabledServerDisabled(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	parked := server("ps_parked", "parked", library.MCPServer{Transport: "stdio", Command: "example-parked"})
	units, unsupported := export(t, tg, []library.Item{parked})
	if len(units) != 1 || len(unsupported) != 0 {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	if want := "[mcp_servers.parked]\ncommand = \"example-parked\"\nenabled = false"; string(units[0].Content) != want {
		t.Errorf("content %q", units[0].Content)
	}
}

func TestNativeEnvRefOnExport(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.Codex)
		mcp := library.MCPServer{
			Transport: "http", URL: "https://mcp.example.com/mcp",
			Headers: map[string]library.Value{"Authorization": {Literal: "Bearer ${env:REMOTE_TOKEN}"}, "X-Key": {Ref: "env:REMOTE_KEY"}},
		}
		stdio := library.MCPServer{Transport: "stdio", Command: "example", Env: map[string]library.Value{"API_TOKEN": {Ref: "env:API_TOKEN"}}}
		cases := []struct {
			original library.MCPServer
			want     string
		}{
			{mcp, "[mcp_servers.fresh]\nurl = \"https://mcp.example.com/mcp\"\nbearer_token_env_var = \"REMOTE_TOKEN\"\nenv_http_headers = { \"X-Key\" = \"REMOTE_KEY\" }"},
			{stdio, "[mcp_servers.fresh]\ncommand = \"example\"\nenv_vars = [\"API_TOKEN\"]"},
		}
		for _, c := range cases {
			original, want := c.original, c.want
			prepared, _, needsSetup, err := secrets.Prepare(original, registered(t).Capabilities().EnvRef, nil)
			if err != nil || len(needsSetup) != 0 {
				t.Fatalf("prepare %v %v", err, needsSetup)
			}
			for _, candidate := range []library.MCPServer{original, prepared} {
				units, unsupported := export(t, tg, []library.Item{server("ps_fresh", "fresh", candidate)})
				if len(units) != 1 || len(unsupported) != 0 {
					t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
				}
				if string(units[0].Content) != want || units[0].Key != "mcp_servers.fresh" {
					t.Errorf("%s unit %s", scope, describe(tg, units))
				}
			}
		}
	}
}

func TestExportNeverWritesSecretsOrOtherPlaceholders(t *testing.T) {
	stdio := func(env map[string]library.Value, args ...string) library.MCPServer {
		return library.MCPServer{Transport: "stdio", Command: "example", Args: args, Env: env}
	}
	http := func(url string, headers map[string]library.Value) library.MCPServer {
		return library.MCPServer{Transport: "http", URL: url, Headers: headers}
	}
	resolved := stdio(map[string]library.Value{"DB": {Literal: "fixture-resolved-value"}})
	resolved.Resolve = true
	cases := map[string]library.MCPServer{
		"renamed variable":   stdio(map[string]library.Value{"TOKEN": {Ref: "env:OTHER"}}),
		"embedded in env":    stdio(map[string]library.Value{"DSN": {Literal: "db://${env:HOST}"}}),
		"argument":           stdio(nil, "--root=${env:ROOT}"),
		"url":                http("https://example.com/${env:PART}", nil),
		"header prefix":      http("https://example.com", map[string]library.Value{"X-Key": {Literal: "Key ${env:KEY}"}}),
		"keychain":           stdio(map[string]library.Value{"DB": {Ref: "keychain:example"}}),
		"needs setup":        stdio(map[string]library.Value{"DB": {Ref: "needs-setup:DB"}}),
		"resolved":           resolved,
		"literal secret":     stdio(map[string]library.Value{"API_KEY": {Literal: "fixture-secret-value"}}),
		"literal bearer":     http("https://example.com", map[string]library.Value{"Authorization": {Literal: "Bearer fixture-secret-value"}}),
		"secret argument":    stdio(nil, "--api-key", "fixture-secret-value"),
		"sse":                {Transport: "sse", URL: "https://example.com"},
		"headers on stdio":   {Transport: "stdio", Command: "x", Headers: map[string]library.Value{"X": {Literal: "y"}}},
		"command with a url": {Transport: "http", URL: "https://example.com", Command: "x"},
	}
	for name, mcp := range cases {
		t.Run(name, func(t *testing.T) {
			tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
			units, unsupported := export(t, tg, []library.Item{server("ps_x", "fresh", mcp)})
			if len(units) != 0 || len(unsupported) != 1 || unsupported[0].Reason == "" {
				t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
			}
			if strings.Contains(unsupported[0].Reason, "fixture-") {
				t.Error("the reason carries a value")
			}
		})
	}
}

func TestUnsupportedReported(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	items := []library.Item{
		{ID: "ps_agent", Kind: library.KindRule, Name: "Releases", Activation: library.ActivationAgent, Body: "## Releases\n"},
		{ID: "ps_skill", Kind: library.KindSkill, Name: "silent", Body: "x\n"},
		{ID: "ps_cmd", Kind: library.KindCommand, Name: "mute", Body: "x\n"},
		{ID: "ps_sub", Kind: library.KindSubagent, Name: "nameless", Body: "x\n"},
		{ID: "ps_empty", Kind: library.KindSubagent, Name: "empty", Description: "Nothing."},
		{ID: "ps_allow", Kind: library.KindPermission, Name: "Allow", Portable: &library.PortablePermission{AllowMCPServers: []string{"docs"}}},
		{ID: "ps_foreign", Kind: library.KindPermission, Name: "Claude only", Native: map[string]any{adapter.ClaudeCode: map[string]any{}}},
		{ID: "ps_hook", Kind: library.KindHook, Name: "Other tool", Native: map[string]any{adapter.ClaudeCode: map[string]any{}}},
		{ID: "ps_event", Kind: library.KindHook, Name: "Bad event", Native: map[string]any{adapter.Codex: map[string]any{"event": "Notification", "hooks": []any{map[string]any{"type": "command", "command": "x"}}}}},
		{ID: "ps_tool", Kind: library.KindHook, Name: "No command", Native: map[string]any{adapter.Codex: map[string]any{"event": "Stop", "hooks": []any{map[string]any{"type": "mcp_tool"}}}}},
		server("ps_name", "has space", library.MCPServer{Transport: "stdio", Command: "x"}),
	}
	units, unsupported := export(t, tg, items)
	if len(units) != 0 {
		t.Errorf("units %s", describe(tg, units))
	}
	var got []string
	for _, u := range unsupported {
		if u.Tool != adapter.Codex || u.Blocking {
			t.Errorf("unsupported %+v", u)
		}
		got = append(got, string(u.ItemID)+": "+u.Reason)
	}
	testenv.Golden(t, "testdata/golden/unsupported.txt", []byte(strings.Join(got, "\n")+"\n"))
}

func TestSubagentFieldsWithoutCodexEquivalentReported(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	items := []library.Item{{ID: "ps_sub", Kind: library.KindSubagent, Name: "reader", Description: "Reads.", Meta: map[string]any{"tools": "Read", "model": "sonnet"}, Body: "Read.\n"}}
	units, unsupported := export(t, tg, items)
	want := []adapter.Unsupported{{ItemID: "ps_sub", Name: "reader", Tool: adapter.Codex, Reason: "written without model, tools, which Codex agent files do not take"}}
	if len(units) != 1 || !reflect.DeepEqual(unsupported, want) {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	if bytes.Contains(units[0].Content, []byte("sonnet")) {
		t.Errorf("content %s", units[0].Content)
	}
}

func TestSubagentInstructionsSurviveTOML(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	for _, body := range []string{"Plain.\n", "Quote \"\"\"three\"\"\" and \"\"\"\"four\"\"\"\".\n", "Ends with a quote \"", "\nLeading newline.\n", "Back\\slash \\n stays.\n", "Tab\tand bell\a.\n"} {
		units, unsupported := export(t, tg, []library.Item{{ID: "ps_sub", Kind: library.KindSubagent, Name: "x", Description: "d \"q\"", Body: body}})
		if len(units) != 1 || len(unsupported) != 0 {
			t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
		}
		var decoded map[string]any
		if err := toml.Unmarshal(units[0].Content, &decoded); err != nil {
			t.Fatalf("%q: %v\n%s", body, err, units[0].Content)
		}
		if decoded["developer_instructions"] != body || decoded["description"] != "d \"q\"" {
			t.Errorf("%q decoded as %q", body, decoded["developer_instructions"])
		}
	}
}

func TestExportSkillWithCredentialAttachmentReported(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.Codex)
	items := []library.Item{
		{ID: "ps_bad", Kind: library.KindSkill, Name: "../escape", Description: "d", Body: "x\n"},
		{ID: "ps_cred", Kind: library.KindSkill, Name: "tokens", Description: "d", Body: "x\n", Files: map[string]library.File{"scripts/token.json": {Data: []byte("{}")}}},
	}
	units, unsupported := export(t, tg, items)
	if len(units) != 0 || len(unsupported) != 2 {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestBackupSet(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.Codex)
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
			tg := target(t, "roundtrip", scope, adapter.Codex, adapter.AgentsMD)
			homeBefore, projectBefore := testenv.TreeHash(t, tg.Env.Home), testenv.TreeHash(t, tg.ProjectRoot)
			imported := importFrom(t, tg)
			if len(imported.Items) == 0 || len(imported.Notes) != 0 {
				t.Fatalf("imported %s", importYAML(t, tg, imported))
			}
			units, unsupported := export(t, tg, imported.Items)
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
