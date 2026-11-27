package opencode_test

import (
	"bytes"
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
	_ "github.com/antonL95/promptstor/internal/adapter/opencode"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	homeMarker    = "<home>"
	projectMarker = "<project>"

	partialReason = "opencode path denies cover only the read and edit tools; shell and grep are not covered"
)

func registered(t *testing.T) adapter.Adapter {
	t.Helper()
	a, ok := adapter.Get(adapter.OpenCode)
	if !ok {
		t.Fatal("opencode is not registered")
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

func emptyTarget(t *testing.T, scope adapter.Scope) adapter.Target {
	t.Helper()
	return adapter.Target{Env: testenv.Home(t, t.TempDir()), Scope: scope, ProjectRoot: t.TempDir(), Selected: []adapter.ToolID{adapter.OpenCode}}
}

func configDir(tg adapter.Target, parts ...string) string {
	return filepath.Join(append([]string{tg.Env.Home, ".config", "opencode"}, parts...)...)
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
	changes, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.OpenCode}, plan.Options{})
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
		view.Items = append(view.Items, itemView{
			Kind: it.Kind, Name: it.Name, Order: it.Order, Source: relative(tg, it.Source), Description: it.Description,
			Meta: it.Meta, Files: slices.Sorted(maps.Keys(it.Files)), Portable: it.Portable, MCP: it.MCP, Body: it.Body,
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
			u.Kind, relative(tg, u.Path), u.Key, relative(tg, u.Root), u.Mode, strings.Join(items, ","), relative(tg, string(u.Content)))
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

func rule(id, name string) library.Item {
	return library.Item{ID: library.ID(id), Kind: library.KindRule, Name: name, Body: "## " + name + "\n\nText.\n"}
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

func unsupportedFor(id, name, reason string, blocking bool) adapter.Unsupported {
	return adapter.Unsupported{ItemID: library.ID(id), Name: name, Tool: adapter.OpenCode, Reason: reason, Blocking: blocking}
}

func libraryItems() []library.Item {
	return []library.Item{
		rule("ps_reviews", "Reviews"),
		rule("ps_commits", "Commits"),
		{ID: "ps_skill", Kind: library.KindSkill, Name: "release-notes", Description: "Draft release notes.", Body: "Summarise the changes.\n", Files: map[string]library.File{"scripts/run.sh": {Data: []byte("#!/bin/sh\n"), Mode: 0o755}}},
		{ID: "ps_command", Kind: library.KindCommand, Name: "git:commit", Description: "Write a commit message", Meta: map[string]any{"opencode_agent": "build", "opencode_subagent": false}, Body: "Write a commit message.\n"},
		{ID: "ps_sub", Kind: library.KindSubagent, Name: "code-reviewer", Description: "Reviews diffs.", Meta: map[string]any{"opencode_model": "example-provider/example-model", "opencode_steps": 5}, Body: "Be strict.\n"},
		guard([]string{"git push --force *", "rm -rf"}, []string{"/etc/example/**", "~/.ssh/**"}),
		server("ps_mcp", "tracker", library.MCPServer{Transport: "stdio", Command: "example-tracker", Args: []string{"--verbose"}, Env: map[string]library.Value{"MODE": {Literal: "readonly"}}}),
		server("ps_remote", "remote", library.MCPServer{Transport: "http", URL: "https://mcp.example.com/mcp", Headers: map[string]library.Value{"X-Team": {Literal: "a"}}}),
	}
}

func TestDetect(t *testing.T) {
	a := registered(t)
	if a.ID() != adapter.OpenCode {
		t.Errorf("ID %q", a.ID())
	}
	env := testenv.Home(t, t.TempDir())
	if got := a.Detect(env); got.Installed || len(got.Paths) != 0 {
		t.Errorf("empty home %+v", got)
	}
	dir := filepath.Join(env.ConfigHome, "opencode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := a.Detect(env); !got.Installed || !reflect.DeepEqual(got.Paths, []string{dir}) {
		t.Errorf("config dir %+v", got)
	}
	env = testenv.Home(t, t.TempDir())
	env.LookPath = func(name string) (string, error) {
		if name == "opencode" {
			return "/usr/local/bin/opencode", nil
		}
		return "", exec.ErrNotFound
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
	tg := target(t, "basic", adapter.ScopeUser, adapter.OpenCode)
	before := testenv.TreeHash(t, tg.Env.Home)
	testenv.Golden(t, "testdata/golden/import-user.yaml", importYAML(t, tg, importFrom(t, tg)))
	if testenv.TreeHash(t, tg.Env.Home) != before {
		t.Error("import changed the home")
	}
}

func TestImportProjectScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.OpenCode, adapter.AgentsMD)
	testenv.Golden(t, "testdata/golden/import-project.yaml", importYAML(t, tg, importFrom(t, tg)))
}

func TestSecretsScrubbedOnImport(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.OpenCode)
	imported := importFrom(t, tg)
	report := importYAML(t, tg, imported)
	for _, secret := range []string{"fixture-token-value", "fixture-provider-secret", "fixture-agent-secret", "fixture-service-secret", "fixture-auth-secret"} {
		if bytes.Contains(report, []byte(secret)) {
			t.Errorf("import carries %s", secret)
		}
	}
	tracker := find(t, imported.Items, library.KindMCP, "tracker")
	if tracker.MCP.Env["MODE"] != (library.Value{Literal: "readonly"}) || tracker.MCP.Env["TRACKER_TOKEN"].Literal != "" {
		t.Errorf("tracker %+v", tracker.MCP)
	}
}

func TestImportMergesJSONThenJSONC(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	write(t, configDir(tg, "opencode.json"), `{"mcp":{"servers":{"shared":{"type":"local","command":["from-json"]},"only-json":{"type":"local","command":["a"]}}},"permissions":[{"action":"shell","resource":"one *","effect":"deny"}]}`)
	write(t, configDir(tg, "opencode.jsonc"), "{\n  // wins\n  \"mcp\": {\"servers\": {\"shared\": {\"type\": \"local\", \"command\": [\"from-jsonc\"]}}},\n  \"permissions\": [{\"action\": \"shell\", \"resource\": \"two *\", \"effect\": \"deny\"}],\n}\n")
	imported := importFrom(t, tg)
	if got := find(t, imported.Items, library.KindMCP, "shared"); got.MCP.Command != "from-jsonc" || got.Source != configDir(tg, "opencode.jsonc") {
		t.Errorf("shared %+v %s", got.MCP, got.Source)
	}
	find(t, imported.Items, library.KindMCP, "only-json")
	permission := find(t, imported.Items, library.KindPermission, "opencode permissions")
	if !reflect.DeepEqual(permission.Portable.DenyCommands, []string{"one", "two"}) {
		t.Errorf("commands %v", permission.Portable.DenyCommands)
	}
}

func TestV1KeysReadV2Written(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.OpenCode)
	imported := importFrom(t, tg)
	permission := find(t, imported.Items, library.KindPermission, "opencode permissions")
	if !reflect.DeepEqual(permission.Portable.DenyCommands, []string{"rm -rf", "git push --force"}) {
		t.Errorf("commands %v", permission.Portable.DenyCommands)
	}
	find(t, imported.Items, library.KindCommand, "triage")
	find(t, imported.Items, library.KindSubagent, "scribe")
	tracker := find(t, imported.Items, library.KindMCP, "tracker")
	units, unsupported := export(t, emptyTarget(t, adapter.ScopeUser), permission, tracker, find(t, imported.Items, library.KindCommand, "triage"))
	for _, u := range unsupported {
		if u.Reason != partialReason && !strings.Contains(u.Reason, "needs setup") {
			t.Errorf("unsupported %+v", u)
		}
	}
	var keys []string
	for _, u := range units {
		keys = append(keys, u.Key+" "+string(u.Content))
	}
	joined := strings.Join(keys, "\n")
	for _, v1 := range []string{`"bash"`, "/permission/", "/command/", "/agent/", "/tools", "/mcp/tracker", `"enabled"`} {
		if strings.Contains(joined, v1) {
			t.Errorf("V1 form %s written:\n%s", v1, joined)
		}
	}
	if !strings.Contains(joined, `{"action":"shell","resource":"rm -rf *","effect":"deny"}`) {
		t.Errorf("no shell deny:\n%s", joined)
	}
}

func TestExportUserScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.OpenCode)
	items := libraryItems()
	units, unsupported := export(t, tg, items...)
	testenv.Golden(t, "testdata/golden/units-user.txt", describe(tg, units))
	if want := []adapter.Unsupported{unsupportedFor("ps_perm", "Guard", partialReason, true)}; !reflect.DeepEqual(unsupported, want) {
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
	want := map[string]string{
		"<home>/.config/opencode/AGENTS.md":              "symlinked -> ../../dotfiles/AGENTS.md",
		"<home>/.agents/skills/release-notes/SKILL.md":   "not created by promptstor",
		"<home>/.config/opencode/commands/git/commit.md": "not created by promptstor",
	}
	if !maps.Equal(blocked, want) {
		t.Errorf("blocked %v", blocked)
	}
	if testenv.TreeHash(t, tg.Env.Home) != before {
		t.Error("the home changed")
	}
}

func TestExportProjectScope(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.OpenCode, adapter.ClaudeCode, adapter.AgentsMD)
	only := rule("ps_only", "opencode notes")
	only.Targets = library.Targets{Include: []string{adapter.OpenCode}}
	items := append(libraryItems(), only)
	units, unsupported := export(t, tg, items...)
	testenv.Golden(t, "testdata/golden/units-project.txt", describe(tg, units))
	want := []adapter.Unsupported{
		unsupportedFor("ps_only", "opencode notes", "opencode reads only AGENTS.md in a project", false),
		unsupportedFor("ps_perm", "Guard", partialReason, true),
	}
	if !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}
}

func TestWritesOnlyJSONC(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.OpenCode)
		units, _ := export(t, tg, libraryItems()...)
		configs := 0
		for _, u := range units {
			if u.Kind == manifest.UnitJSON || u.Kind == manifest.UnitJSONElement {
				configs++
				want := configDir(tg, "opencode.jsonc")
				if scope == adapter.ScopeProject {
					want = filepath.Join(tg.ProjectRoot, "opencode.jsonc")
				}
				if u.Path != want {
					t.Errorf("%s config unit in %s", scope, relative(tg, u.Path))
				}
			}
			if filepath.Base(u.Path) == "opencode.json" {
				t.Errorf("%s writes opencode.json", scope)
			}
		}
		if configs != 8 {
			t.Errorf("%s config units %d", scope, configs)
		}
	}
}

func TestPermissionRulesAppendedLast(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	config := configDir(tg, "opencode.jsonc")
	write(t, config, "{\n  \"permissions\": [\n    {\"action\": \"shell\", \"resource\": \"*\", \"effect\": \"allow\"}\n  ]\n}\n")
	units, unsupported := export(t, tg, guard([]string{"rm -rf"}, nil))
	if len(units) != 1 || len(unsupported) != 0 {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	hash, err := manifest.Hash(manifest.UnitJSONElement, units[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	if units[0].Kind != manifest.UnitJSONElement || units[0].Key != "/permissions@#"+hash {
		t.Errorf("unit %s", describe(tg, units))
	}
	changes := diff(t, units)
	if len(changes) != 1 || changes[0].Blocked != "" {
		t.Fatalf("changes %+v", changes)
	}
	after := string(changes[0].After)
	allow, deny := strings.Index(after, `"allow"`), strings.Index(after, `"rm -rf *"`)
	if allow < 0 || deny < allow {
		t.Errorf("deny not last:\n%s", after)
	}
}

func TestJSONCCommentsKept(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.OpenCode)
	units, _ := export(t, tg, guard([]string{"rm -rf"}, nil), server("ps_mcp", "tracker", local("example-tracker")))
	changes := diff(t, units)
	if len(changes) != 1 || changes[0].Blocked != "" {
		t.Fatalf("changes %+v", changes)
	}
	for _, kept := range []string{"// local additions go here", `"$schema": "https://opencode.ai/config.json"`, `"rm -rf *"`, `"example-tracker"`} {
		if !bytes.Contains(changes[0].After, []byte(kept)) {
			t.Errorf("missing %s in\n%s", kept, changes[0].After)
		}
	}
}

func TestSymlinkedAgentsMDBlocked(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.OpenCode)
	units, _ := export(t, tg, rule("ps_reviews", "Reviews"))
	if len(units) != 1 || units[0].Path != configDir(tg, "AGENTS.md") || units[0].Kind != manifest.UnitSection {
		t.Fatalf("units %s", describe(tg, units))
	}
	changes := diff(t, units)
	if len(changes) != 1 || changes[0].Blocked != "symlinked -> ../../dotfiles/AGENTS.md" {
		t.Errorf("changes %+v", changes)
	}
}

func TestSkillsWrittenToSharedHome(t *testing.T) {
	skill := library.Item{ID: "ps_skill", Kind: library.KindSkill, Name: "release-notes", Description: "Draft.", Body: "x\n"}
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := emptyTarget(t, scope)
		units, unsupported := export(t, tg, skill)
		shared, err := adapter.SkillUnits(tg, adapter.OpenCode, skill)
		if err != nil {
			t.Fatal(err)
		}
		if len(unsupported) != 0 || !reflect.DeepEqual(units, shared) || !strings.Contains(units[0].Path, filepath.Join(".agents", "skills")) {
			t.Errorf("%s units %s unsupported %+v", scope, describe(tg, units), unsupported)
		}
	}
}

func TestNativeEnvRefOnExport(t *testing.T) {
	if got := registered(t).Capabilities().EnvRef("NAME"); got != "{env:NAME}" {
		t.Errorf("env ref %q", got)
	}
	tg := emptyTarget(t, adapter.ScopeUser)
	units, unsupported := export(t, tg,
		server("ps_local", "a", library.MCPServer{Transport: "stdio", Command: "x", Args: []string{"--key=${env:API_KEY}"}, Env: map[string]library.Value{"API_KEY": {Ref: "env:API_KEY"}}}),
		server("ps_remote", "b", library.MCPServer{Transport: "http", URL: "https://example.com/?r=${env:REGION}", Headers: map[string]library.Value{"Authorization": {Literal: "Bearer ${env:TOKEN}"}}}),
		server("ps_setup", "c", library.MCPServer{Transport: "stdio", Command: "x", Env: map[string]library.Value{"API_KEY": {Ref: "keychain:example/account"}}}),
	)
	want := []string{
		`{"type":"local","command":["x","--key={env:API_KEY}"],"environment":{"API_KEY":"{env:API_KEY}"}}`,
		`{"type":"remote","url":"https://example.com/?r={env:REGION}","headers":{"Authorization":"Bearer {env:TOKEN}"}}`,
	}
	var got []string
	for _, u := range units {
		got = append(got, string(u.Content))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("units %v", got)
	}
	setup := []adapter.Unsupported{unsupportedFor("ps_setup", "c", "secret env/API_KEY needs setup before opencode can use it", false)}
	if !reflect.DeepEqual(unsupported, setup) {
		t.Errorf("unsupported %+v", unsupported)
	}
}

func TestFileReferencesRefused(t *testing.T) {
	reason := "the server uses {file:...}, which would make opencode read a local file into it"
	for label, mcp := range map[string]library.MCPServer{
		"header":  {Transport: "http", URL: "https://example.com", Headers: map[string]library.Value{"X": {Literal: "{file:~/.ssh/id}"}}},
		"url":     {Transport: "http", URL: "https://example.com/{file:key}"},
		"args":    {Transport: "stdio", Command: "x", Args: []string{"{file:key}"}},
		"env":     {Transport: "stdio", Command: "x", Env: map[string]library.Value{"A": {Literal: "{file:key}"}}},
		"command": {Transport: "stdio", Command: "{file:key}"},
	} {
		tg := emptyTarget(t, adapter.ScopeUser)
		units, unsupported := export(t, tg, server("ps_mcp", "s", mcp))
		if len(units) != 0 || !reflect.DeepEqual(unsupported, []adapter.Unsupported{unsupportedFor("ps_mcp", "s", reason, false)}) {
			t.Errorf("%s units %s unsupported %+v", label, describe(tg, units), unsupported)
		}
	}
	tg := emptyTarget(t, adapter.ScopeUser)
	write(t, configDir(tg, "opencode.jsonc"), `{"mcp":{"servers":{"s":{"type":"remote","url":"https://example.com","headers":{"X":"{file:key}"}}}}}`)
	imported := importFrom(t, tg)
	if len(imported.Items) != 0 || len(imported.Notes) != 1 {
		t.Errorf("imported %s", importYAML(t, tg, imported))
	}
}

func TestFileReferencesInNamesRefused(t *testing.T) {
	for label, item := range map[string]library.Item{
		"server name": server("ps_mcp", "{file:key}", local("x")),
		"env name":    server("ps_mcp", "s", library.MCPServer{Transport: "stdio", Command: "x", Env: map[string]library.Value{"{file:key}": {Literal: "a"}}}),
		"header name": server("ps_mcp", "s", library.MCPServer{Transport: "http", URL: "https://example.com", Headers: map[string]library.Value{"{file:key}": {Literal: "a"}}}),
	} {
		tg := emptyTarget(t, adapter.ScopeUser)
		units, unsupported := export(t, tg, item)
		if len(units) != 0 || len(unsupported) != 1 || !strings.Contains(unsupported[0].Reason, "{file:...}") {
			t.Errorf("%s units %s unsupported %+v", label, describe(tg, units), unsupported)
		}
	}
}

func TestNestedModeFilesIgnored(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	write(t, configDir(tg, "modes", "top.md"), "Top.\n")
	write(t, configDir(tg, "modes", "team", "nested.md"), "Nested.\n")
	imported := importFrom(t, tg)
	if len(imported.Items) != 1 || imported.Items[0].Name != "top" || imported.Items[0].Meta["opencode_mode"] != "primary" {
		t.Errorf("imported %s", importYAML(t, tg, imported))
	}
}

func TestServerSettingsKept(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	write(t, configDir(tg, "opencode.json"), `{"mcp":{"docs":{"type":"local","command":["old"],"enabled":false,"timeout":5000},"signed":{"type":"remote","url":"https://signed.example.com","oauth":{"clientId":"a","clientSecret":"fixture-oauth-secret"}}}}`)
	write(t, configDir(tg, "opencode.jsonc"), `{"mcp":{"servers":{"kept":{"type":"local","command":["old"],"cwd":"tools","codemode":false}}}}`)
	units, unsupported := export(t, tg,
		server("ps_docs", "docs", local("new")),
		server("ps_kept", "kept", local("new")),
		server("ps_signed", "signed", library.MCPServer{Transport: "http", URL: "https://signed.example.com"}),
	)
	var got []string
	for _, u := range units {
		got = append(got, u.Key+" "+string(u.Content))
	}
	want := []string{
		`/mcp/servers/docs {"type":"local","command":["new"],"disabled":true,"timeout":{"catalog":5000,"execution":5000,"startup":5000}}`,
		`/mcp/servers/kept {"type":"local","command":["new"],"cwd":"tools","codemode":false}`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("units %v", got)
	}
	dropped := []adapter.Unsupported{unsupportedFor("ps_signed", "signed", "mcp server signed has oauth, which PromptStor would drop; set the server up by hand", false)}
	if !reflect.DeepEqual(unsupported, dropped) {
		t.Errorf("unsupported %+v", unsupported)
	}
}

func TestShadowedByDotOpencode(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeProject, adapter.OpenCode)
	units, unsupported := export(t, tg, server("ps_lint", "lint", local("example-lint")))
	want := []adapter.Unsupported{unsupportedFor("ps_lint", "lint", ".opencode/opencode.jsonc already defines mcp server lint and overrides the project file; not written", false)}
	if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestInexpressibleDeniesBlocked(t *testing.T) {
	check := func(label string, item library.Item) {
		tg := emptyTarget(t, adapter.ScopeUser)
		units, unsupported := export(t, tg, item)
		if len(units) != 0 || len(unsupported) != 1 || !unsupported[0].Blocking {
			t.Errorf("%q units %s unsupported %+v", label, describe(tg, units), unsupported)
		}
	}
	for _, command := range []string{"", "  ", "*", "* *", `rm "my file"`, "echo 'x'", "a && b", "a | b", "a; b", "a\nb", "echo {env:X}"} {
		check(command, guard([]string{command}, nil))
	}
	for _, path := range []string{"", "!keep", "a{b,c}", "a[0]", "~other/x", "secrets/", "a\nb"} {
		check(path, guard(nil, []string{path}))
	}
}

func TestDenyGapsNeedAcknowledgement(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	if err := os.MkdirAll(filepath.Join(tg.Env.Home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	units, unsupported := export(t, tg, guard(nil, []string{"**/.env", "secrets/*", "~/.ssh"}))
	var got []string
	for _, u := range unsupported {
		if !u.Blocking {
			t.Errorf("unsupported %+v", u)
		}
		got = append(got, u.Reason)
	}
	want := []string{
		`deny path "**/.env" does not match at the top of the workspace`,
		`deny path "secrets/*" is matched against paths relative to the working directory only`,
		`deny path "~/.ssh" names a folder, and opencode matches only that exact path; end it with /**`,
		partialReason,
	}
	if len(units) != 6 || !reflect.DeepEqual(got, want) {
		t.Errorf("units %d reasons %q", len(units), got)
	}
}

func TestUnmatchableV2ShellRuleNoted(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.OpenCode)
	imported := importFrom(t, tg)
	want := adapter.Note{Path: configDir(tg, "opencode.json"), Message: `permissions rule bash "sudo *" never matches, because opencode V2 calls the action shell; not imported`}
	if !slices.Contains(imported.Notes, want) {
		t.Errorf("notes %+v", imported.Notes)
	}
}

func TestSubagentFrontmatter(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	item := library.Item{ID: "ps_sub", Kind: library.KindSubagent, Name: "Code Reader", Description: "Reads.", Meta: map[string]any{
		"model": "sonnet", "tools": "Read", "opencode_model": "sonnet", "opencode_color": "#00ff00", "opencode_hidden": true,
		"opencode_permissions": []any{map[string]any{"action": "edit", "resource": "*", "effect": "deny"}},
	}, Body: "Read.\n"}
	units, unsupported := export(t, tg, item)
	want := []adapter.Unsupported{unsupportedFor("ps_sub", "Code Reader", "written without model, opencode_model, tools, which opencode agent files do not take", false)}
	if len(units) != 1 || !reflect.DeepEqual(unsupported, want) {
		t.Fatalf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
	content := "---\ndescription: Reads.\nmode: subagent\nhidden: true\ncolor: '#00ff00'\npermissions:\n  - action: edit\n    resource: '*'\n    effect: deny\n---\n" + managed.Notice + "\nRead.\n"
	if got := string(units[0].Content); got != content || units[0].Path != configDir(tg, "agents", "code-reader.md") {
		t.Errorf("%s content %q", units[0].Path, got)
	}
}

func TestExistingDefinitionsNotDuplicated(t *testing.T) {
	tg := target(t, "basic", adapter.ScopeUser, adapter.OpenCode)
	units, unsupported := export(t, tg,
		library.Item{ID: "ps_sub", Kind: library.KindSubagent, Name: "planner", Description: "Plans.", Body: "Plan.\n"},
		library.Item{ID: "ps_cmd", Kind: library.KindCommand, Name: "legacy", Body: "x\n"},
		library.Item{ID: "ps_builtin", Kind: library.KindSubagent, Name: "general", Description: "x", Body: "x\n"},
	)
	want := []adapter.Unsupported{
		unsupportedFor("ps_sub", "planner", "agent/planner.md already defines this name; not written", false),
		unsupportedFor("ps_cmd", "legacy", "command/legacy.md already defines this name; not written", false),
		unsupportedFor("ps_builtin", "general", `"general" is a built in opencode agent name`, false),
	}
	if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestUnsupportedReported(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	scoped := rule("ps_files", "Go style")
	scoped.Activation, scoped.Scope = library.ActivationFiles, []string{"**/*.go"}
	agentRule := rule("ps_agent", "Releases")
	agentRule.Activation = library.ActivationAgent
	items := []library.Item{
		scoped,
		agentRule,
		{ID: "ps_skill", Kind: library.KindSkill, Name: "../escape", Description: "x", Body: "x\n"},
		{ID: "ps_cred", Kind: library.KindSkill, Name: "keys", Description: "x", Body: "x\n", Files: map[string]library.File{"scripts/token.json": {Data: []byte("{}")}}},
		{ID: "ps_cmd", Kind: library.KindCommand, Name: "mute", Body: "  "},
		{ID: "ps_cmdname", Kind: library.KindCommand, Name: "!!", Body: "x\n"},
		{ID: "ps_cmdmeta", Kind: library.KindCommand, Name: "hinted", Meta: map[string]any{"argument-hint": "[x]"}, Body: "x\n"},
		{ID: "ps_subname", Kind: library.KindSubagent, Name: "??", Body: "x\n"},
		{ID: "ps_allow", Kind: library.KindPermission, Name: "Servers", Portable: &library.PortablePermission{AllowMCPServers: []string{"docs"}}},
		{ID: "ps_native", Kind: library.KindPermission, Name: "Native", Native: map[string]any{adapter.OpenCode: map[string]any{}}},
		{ID: "ps_foreign", Kind: library.KindPermission, Name: "Claude only", Native: map[string]any{adapter.ClaudeCode: map[string]any{}}},
		{ID: "ps_hook", Kind: library.KindHook, Name: "Session note", Native: map[string]any{adapter.ClaudeCode: map[string]any{}}},
		{ID: "ps_nomcp", Kind: library.KindMCP, Name: "empty"},
		server("ps_sse", "sse", library.MCPServer{Transport: "sse", URL: "https://example.com/sse"}),
		server("ps_both", "both", library.MCPServer{Transport: "http", URL: "https://example.com", Command: "x"}),
		server("ps_bare", "bare", library.MCPServer{Transport: "stdio"}),
	}
	units, unsupported := export(t, tg, items...)
	if len(units) != 1 || filepath.Base(units[0].Path) != "hinted.md" {
		t.Errorf("units %s", describe(tg, units))
	}
	var got []string
	for _, u := range unsupported {
		if u.Tool != adapter.OpenCode || u.Blocking {
			t.Errorf("unsupported %+v", u)
		}
		got = append(got, string(u.ItemID)+": "+u.Reason)
	}
	testenv.Golden(t, "testdata/golden/unsupported.txt", []byte(strings.Join(got, "\n")+"\n"))
}

func TestConfigDirOverride(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	outside := filepath.Join(t.TempDir(), "oc")
	write(t, filepath.Join(outside, "opencode.jsonc"), "{}\n")
	tg.Env.Getenv = func(name string) string {
		if name == "OPENCODE_CONFIG_DIR" {
			return outside
		}
		return ""
	}
	if got := registered(t).Detect(tg.Env); !got.Installed || !reflect.DeepEqual(got.Paths, []string{outside}) {
		t.Errorf("detection %+v", got)
	}
	units, _ := export(t, tg, rule("ps_reviews", "Reviews"), server("ps_mcp", "s", local("x")))
	for _, u := range units {
		if filepath.Dir(u.Path) != outside || u.Root != filepath.Dir(outside) {
			t.Errorf("unit %s root %s", u.Path, u.Root)
		}
	}
	if len(units) != 2 {
		t.Errorf("units %s", describe(tg, units))
	}
	tg.Env.Getenv = func(string) string { return "relative/dir" }
	units, unsupported := export(t, tg, rule("ps_reviews", "Reviews"))
	want := []adapter.Unsupported{unsupportedFor("ps_reviews", "Reviews", "OPENCODE_CONFIG_DIR is not an absolute path, so the opencode config folder is unknown", false)}
	if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %s unsupported %+v", describe(tg, units), unsupported)
	}
}

func TestGlobalClaudeFallbackNoted(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	claude := filepath.Join(tg.Env.Home, ".claude", "CLAUDE.md")
	write(t, claude, "## Other\n\nText.\n")
	imported := importFrom(t, tg)
	want := []adapter.Note{{Path: claude, Message: "opencode loads this while its AGENTS.md is missing; not imported"}}
	if len(imported.Items) != 0 || !reflect.DeepEqual(imported.Notes, want) {
		t.Errorf("imported %s", importYAML(t, tg, imported))
	}
}

func TestCredentialLinksNeverRead(t *testing.T) {
	tg := emptyTarget(t, adapter.ScopeUser)
	secret := filepath.Join(tg.Env.Home, ".local", "share", "opencode", "auth.json")
	write(t, secret, `{"mcp":{"servers":{"leak":{"type":"local","command":["fixture-linked-secret"]}}}}`)
	for _, name := range []string{"AGENTS.md", "opencode.json"} {
		if err := os.MkdirAll(configDir(tg), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(secret, configDir(tg, name)); err != nil {
			t.Fatal(err)
		}
	}
	imported := importFrom(t, tg)
	if len(imported.Items) != 0 || bytes.Contains(importYAML(t, tg, imported), []byte("fixture-linked-secret")) {
		t.Errorf("imported %s", importYAML(t, tg, imported))
	}
}

func TestBackupSet(t *testing.T) {
	for _, scope := range []adapter.Scope{adapter.ScopeUser, adapter.ScopeProject} {
		tg := target(t, "basic", scope, adapter.OpenCode)
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
			tg := target(t, "roundtrip", scope, adapter.OpenCode, adapter.AgentsMD)
			homeBefore, projectBefore := testenv.TreeHash(t, tg.Env.Home), testenv.TreeHash(t, tg.ProjectRoot)
			imported := importFrom(t, tg)
			if len(imported.Items) == 0 || len(imported.Unresolved) != 0 {
				t.Fatalf("imported %s", importYAML(t, tg, imported))
			}
			for _, note := range imported.Notes {
				if !strings.HasPrefix(note.Message, "server ") || !strings.Contains(note.Message, ": field ") {
					t.Errorf("note %+v", note)
				}
			}
			units, unsupported := export(t, tg, imported.Items...)
			for _, u := range unsupported {
				if u.Reason != partialReason {
					t.Errorf("unsupported %+v", u)
				}
			}
			for _, change := range diff(t, units) {
				if change.Op != plan.OpNoop || change.Blocked != "" {
					t.Errorf("%s %s %q\n%s", change.Op, relative(tg, change.Path), change.Blocked, change.After)
				}
			}
			wantUnits := map[adapter.Scope]int{adapter.ScopeUser: 9, adapter.ScopeProject: 4}
			if len(units) != wantUnits[scope] {
				t.Errorf("units %d\n%s", len(units), describe(tg, units))
			}
			if testenv.TreeHash(t, tg.Env.Home) != homeBefore || testenv.TreeHash(t, tg.ProjectRoot) != projectBefore {
				t.Error("the fixture changed")
			}
		})
	}
}
