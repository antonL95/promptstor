package engine_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/policy"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	ruleID       library.ID = "ps_0000000000000000000000ENG1"
	skillID      library.ID = "ps_0000000000000000000000ENG2"
	serverID     library.ID = "ps_0000000000000000000000ENG3"
	permissionID library.ID = "ps_0000000000000000000000ENG4"
	resolved                = "PSFIXTURE-RESOLVED-8N3V"
)

var allTools = []string{adapter.Antigravity, adapter.ClaudeCode, adapter.Codex, adapter.Cursor, adapter.GeminiCLI, adapter.OpenCode}

type resolver map[string]string

func (r resolver) Resolve(ref string) (string, error) {
	if value, found := r[ref]; found {
		return value, nil
	}
	return "", errors.New("no value")
}

type recording struct {
	id          adapter.ToolID
	caps        adapter.Capabilities
	units       func(t adapter.Target) []plan.Unit
	unsupported []adapter.Unsupported
	backup      []plan.SnapshotEntry
	targets     []adapter.Target
	items       map[adapter.Scope][]library.Item
}

func (r *recording) ID() adapter.ToolID                              { return r.id }
func (r *recording) Detect(paths.Env) adapter.Detection              { return adapter.Detection{} }
func (r *recording) Capabilities() adapter.Capabilities              { return r.caps }
func (r *recording) Import(adapter.Target) (adapter.Imported, error) { return adapter.Imported{}, nil }
func (r *recording) BackupSet(t adapter.Target) []plan.SnapshotEntry {
	if t.Scope != adapter.ScopeUser {
		return nil
	}
	return r.backup
}

func (r *recording) Export(t adapter.Target, items []library.Item) ([]plan.Unit, []adapter.Unsupported, error) {
	r.targets = append(r.targets, t)
	if r.items == nil {
		r.items = map[adapter.Scope][]library.Item{}
	}
	r.items[t.Scope] = items
	if r.units == nil || t.Scope != adapter.ScopeUser {
		return nil, nil, nil
	}
	return r.units(t), slices.Clone(r.unsupported), nil
}

func home(t *testing.T) paths.Env {
	t.Helper()
	env := testenv.Home(t, "testdata/home")
	env.StateHome = filepath.Join(t.TempDir(), "state")
	return env
}

func newEngine(env paths.Env, tools ...string) engine.Engine {
	clock := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	return engine.Engine{
		Env:      env,
		Config:   config.Config{Version: 1, Tools: tools},
		Adapters: adapter.All(),
		Resolver: resolver{},
		Now: func() time.Time {
			clock = clock.Add(time.Second)
			return clock
		},
	}
}

func at(env paths.Env, rel string) string {
	return filepath.Join(env.Home, filepath.FromSlash(rel))
}

func fileUnit(env paths.Env, tool, rel, content string, items ...library.ID) plan.Unit {
	return plan.Unit{Tool: tool, Path: at(env, rel), Root: env.Home, Kind: manifest.UnitFile, Content: []byte(content), Mode: 0o644, Items: items}
}

func mustPlan(t *testing.T, e engine.Engine, projectRoot string) engine.Plan {
	t.Helper()
	p, err := e.Plan(projectRoot, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func changeFor(p engine.Plan, path string) (plan.FileChange, bool) {
	for _, c := range p.Changes {
		if c.Path == path {
			return c, true
		}
	}
	return plan.FileChange{}, false
}

func ids(items []library.Item) []library.ID {
	var out []library.ID
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func reportFor(p engine.Plan, id library.ID) []adapter.Unsupported {
	var out []adapter.Unsupported
	for _, u := range p.Unsupported {
		if u.ItemID == id {
			out = append(out, u)
		}
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

func itemByID(t *testing.T, e engine.Engine, id library.ID) library.Item {
	t.Helper()
	merged, _, err := e.Library("")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range merged.Items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no item %s", id)
	return library.Item{}
}

func TestEveryAdapterRegistered(t *testing.T) {
	var got []string
	for _, a := range adapter.All() {
		got = append(got, a.ID())
	}
	want := []string{adapter.AgentsMD, adapter.Antigravity, adapter.ClaudeCode, adapter.Codex, adapter.Cursor, adapter.GeminiCLI, adapter.OpenCode}
	if !slices.Equal(got, want) {
		t.Errorf("registered %v", got)
	}
}

func TestLibraryMergesPersonalDeviceAndProjectLayers(t *testing.T) {
	env := home(t)
	project := t.TempDir()
	writeFile(t, filepath.Join(env.DeviceDir(), "rules", "plain-language.md"), "---\nname: Plain language\npromptstor:\n  id: "+string(ruleID)+"\n---\nDevice wording.\n")
	writeFile(t, filepath.Join(project, ".promptstor", "rules", "project-rule.md"), "---\nname: Project rule\npromptstor:\n  id: ps_0000000000000000000000PRJ1\n---\nProject wording.\n")

	merged, violations, err := newEngine(env).Library(project)
	if err != nil || len(violations) != 0 {
		t.Fatalf("violations %v err %v", violations, err)
	}

	byID := map[library.ID]library.Item{}
	for _, it := range merged.Items {
		byID[it.ID] = it
	}
	if rule := byID[ruleID]; rule.Layer != library.LayerDevice || rule.Body != "Device wording.\n" {
		t.Errorf("rule from %s: %q", rule.Layer, rule.Body)
	}
	if rule := byID["ps_0000000000000000000000PRJ1"]; rule.Layer != library.LayerProject {
		t.Errorf("project rule layer %q", rule.Layer)
	}
	for _, id := range []library.ID{skillID, serverID, permissionID} {
		if byID[id].Layer != library.LayerPersonal {
			t.Errorf("%s layer %q", id, byID[id].Layer)
		}
	}
}

func TestLibraryMissingLayersAreEmpty(t *testing.T) {
	env := home(t)
	env.ConfigHome = t.TempDir()
	merged, violations, err := newEngine(env).Library(t.TempDir())
	if err != nil || len(violations) != 0 || len(merged.Items) != 0 {
		t.Errorf("items %d violations %v err %v", len(merged.Items), violations, err)
	}
}

func TestLockedBuiltinReportsOverride(t *testing.T) {
	env := home(t)
	e := newEngine(env)
	e.Builtins = []library.Item{{ID: ruleID, Kind: library.KindRule, Name: "Plain language", Layer: library.LayerCompany, Locked: true, Body: "Built in.\n"}}

	merged, violations, err := e.Library("")
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].ItemID != ruleID || violations[0].Layer != library.LayerPersonal {
		t.Fatalf("violations %+v", violations)
	}
	count := 0
	for _, it := range merged.Items {
		if it.ID == ruleID {
			count++
			if it.Body != "Built in.\n" {
				t.Errorf("body %q", it.Body)
			}
		}
	}
	if count != 1 {
		t.Errorf("rule appears %d times", count)
	}
}

func TestTargetsCarrySelectedTools(t *testing.T) {
	env := home(t)
	project := t.TempDir()
	claude := &recording{id: adapter.ClaudeCode}
	codex := &recording{id: adapter.Codex}
	agents := &recording{id: adapter.AgentsMD}
	e := newEngine(env, adapter.ClaudeCode, adapter.Codex)
	e.Adapters = []adapter.Adapter{agents, claude, codex}

	mustPlan(t, e, project)

	want := map[adapter.Scope][]string{
		adapter.ScopeUser:    {adapter.ClaudeCode, adapter.Codex},
		adapter.ScopeProject: {adapter.AgentsMD, adapter.ClaudeCode, adapter.Codex},
	}
	for _, r := range []*recording{claude, codex} {
		if len(r.targets) != 2 {
			t.Fatalf("%s exported %d times", r.id, len(r.targets))
		}
		for _, target := range r.targets {
			if !slices.Equal(target.Selected, want[target.Scope]) {
				t.Errorf("%s %s selected %v", r.id, target.Scope, target.Selected)
			}
			if target.Scope == adapter.ScopeProject && target.ProjectRoot != project {
				t.Errorf("project root %q", target.ProjectRoot)
			}
		}
	}
	if len(agents.targets) != 1 || agents.targets[0].Scope != adapter.ScopeProject {
		t.Errorf("agents-md targets %+v", agents.targets)
	}
}

func TestProjectConfigChoosesProjectTools(t *testing.T) {
	env := home(t)
	project := t.TempDir()
	writeFile(t, filepath.Join(project, ".promptstor", "config.yaml"), "tools:\n  - codex\n")
	claude := &recording{id: adapter.ClaudeCode}
	codex := &recording{id: adapter.Codex}
	e := newEngine(env, adapter.ClaudeCode)
	e.Adapters = []adapter.Adapter{&recording{id: adapter.AgentsMD}, claude, codex}

	mustPlan(t, e, project)

	if len(claude.targets) != 1 || claude.targets[0].Scope != adapter.ScopeUser {
		t.Errorf("claude targets %+v", claude.targets)
	}
	if len(codex.targets) != 1 || !slices.Equal(codex.targets[0].Selected, []string{adapter.AgentsMD, adapter.Codex}) {
		t.Errorf("codex targets %+v", codex.targets)
	}
}

func TestUnknownToolRefused(t *testing.T) {
	_, err := newEngine(home(t), "example-tool").Plan("", plan.Options{})
	if err == nil || !strings.Contains(err.Error(), "example-tool") {
		t.Errorf("err %v", err)
	}
}

func TestAgentsMDAddedForAnyProjectTool(t *testing.T) {
	for _, tool := range allTools {
		t.Run(tool, func(t *testing.T) {
			env := home(t)
			project := t.TempDir()
			p := mustPlan(t, newEngine(env, tool), project)
			c, found := changeFor(p, filepath.Join(project, "AGENTS.md"))
			if !found || c.Op != plan.OpCreate || !bytes.Contains(c.After, []byte("Write in plain language.")) {
				t.Errorf("AGENTS.md found %v op %s blocked %q", found, c.Op, c.Blocked)
			}
		})
	}
}

func TestAgentsMDReceivesItemsUnfilteredByTool(t *testing.T) {
	env := home(t)
	project := t.TempDir()
	writeFile(t, filepath.Join(env.LibraryDir(), "rules", "cursor-only.md"), "---\nname: Cursor only\npromptstor:\n  id: ps_0000000000000000000000CUR1\n  targets:\n    include:\n      - cursor\n---\nCursor wording.\n")
	claude := &recording{id: adapter.ClaudeCode}
	agents := &recording{id: adapter.AgentsMD}
	e := newEngine(env, adapter.ClaudeCode)
	e.Adapters = []adapter.Adapter{agents, claude}

	mustPlan(t, e, project)

	if !slices.Contains(ids(agents.items[adapter.ScopeProject]), "ps_0000000000000000000000CUR1") {
		t.Errorf("agents-md items %v", ids(agents.items[adapter.ScopeProject]))
	}
	if slices.Contains(ids(claude.items[adapter.ScopeProject]), "ps_0000000000000000000000CUR1") {
		t.Errorf("claude-code items %v", ids(claude.items[adapter.ScopeProject]))
	}
}

func permissionCaps() adapter.Capabilities {
	return adapter.Capabilities{Kinds: map[library.Kind]bool{library.KindPermission: true, library.KindRule: true, library.KindSkill: true, library.KindMCP: true}}
}

func TestInexpressibleDenyWithheldUntilAcknowledged(t *testing.T) {
	env := home(t)
	claude := &recording{id: adapter.ClaudeCode, caps: permissionCaps()}
	e := newEngine(env, adapter.ClaudeCode)
	e.Adapters = []adapter.Adapter{claude}

	p := mustPlan(t, e, "")

	if slices.Contains(ids(claude.items[adapter.ScopeUser]), permissionID) {
		t.Error("blocked permission passed to export")
	}
	if got := reportFor(p, permissionID); len(got) != 1 || !got[0].Blocking {
		t.Fatalf("unsupported %+v", p.Unsupported)
	}

	e.Acks.Add(policy.Ack{ItemID: permissionID, Tool: adapter.ClaudeCode, Hash: library.ContentHash(itemByID(t, e, permissionID))})
	p = mustPlan(t, e, "")

	if !slices.Contains(ids(claude.items[adapter.ScopeUser]), permissionID) {
		t.Error("acknowledged permission withheld")
	}
	if got := reportFor(p, permissionID); len(got) != 1 || got[0].Blocking || !strings.HasPrefix(got[0].Reason, "acknowledged: ") {
		t.Errorf("unsupported %+v", p.Unsupported)
	}
}

func TestMCPEnvReferencesPreparedBeforeExport(t *testing.T) {
	env := home(t)
	caps := permissionCaps()
	caps.EnvRef = func(name string) string { return "${" + name + "}" }
	claude := &recording{id: adapter.ClaudeCode, caps: caps}
	e := newEngine(env, adapter.ClaudeCode)
	e.Adapters = []adapter.Adapter{claude}

	mustPlan(t, e, "")

	for _, it := range claude.items[adapter.ScopeUser] {
		if it.ID == serverID {
			if got := it.MCP.Env["EXAMPLE_TOKEN"]; got.Literal != "${EXAMPLE_TOKEN}" || got.Ref != "" {
				t.Errorf("EXAMPLE_TOKEN %+v", got)
			}
			return
		}
	}
	t.Error("server not exported")
}

func TestMCPNeedingSetupReportedAndKept(t *testing.T) {
	env := home(t)
	path := at(env, ".example/servers.json")
	writeFile(t, path, "{\"example-docs\": {}}\n")
	m := manifest.Manifest{Version: 1}
	m.Put(manifest.Entry{Tool: adapter.ClaudeCode, Path: path, Root: env.Home, Unit: manifest.UnitJSON, Key: "/example-docs", Hash: "sha256:x", Items: []library.ID{serverID}})
	if err := m.Save(env.ManifestPath()); err != nil {
		t.Fatal(err)
	}
	claude := &recording{id: adapter.ClaudeCode, caps: permissionCaps(), units: func(adapter.Target) []plan.Unit { return nil }}
	e := newEngine(env, adapter.ClaudeCode)
	e.Adapters = []adapter.Adapter{claude}
	e.Acks.Add(policy.Ack{ItemID: permissionID, Tool: adapter.ClaudeCode, Hash: library.ContentHash(itemByID(t, e, permissionID))})

	p := mustPlan(t, e, "")

	if slices.Contains(ids(claude.items[adapter.ScopeUser]), serverID) {
		t.Error("server needing setup passed to export")
	}
	var reported bool
	for _, u := range p.Unsupported {
		if u.ItemID == serverID {
			reported = u.Reason == "needs manual setup: env/EXAMPLE_TOKEN" && !u.Blocking
		}
	}
	if !reported {
		t.Errorf("unsupported %+v", p.Unsupported)
	}
	if c, found := changeFor(p, path); found {
		t.Errorf("entry of server needing setup planned: %+v", c)
	}
}

func TestResolveSecretsMarksUnitsSecret(t *testing.T) {
	env := home(t)
	writeFile(t, filepath.Join(env.LibraryDir(), "mcp", "example-docs.yaml"), "promptstor:\n  id: "+string(serverID)+"\ntransport: stdio\ncommand: example-docs-server\nenv:\n  EXAMPLE_TOKEN:\n    ref: keychain:example-service/example-account\n")
	var got library.Value
	claude := &recording{id: adapter.ClaudeCode, caps: permissionCaps(), units: func(adapter.Target) []plan.Unit {
		return []plan.Unit{{Tool: adapter.ClaudeCode, Path: at(env, ".example/servers.json"), Root: env.Home, Kind: manifest.UnitJSON, Key: "/example-docs", Content: []byte(`{"command":"example-docs-server"}`), Items: []library.ID{serverID}}}
	}}
	e := newEngine(env, adapter.ClaudeCode)
	e.Adapters = []adapter.Adapter{claude}
	e.Config.ResolveSecrets = []string{"example-docs"}
	e.Resolver = resolver{"keychain:example-service/example-account": resolved}

	p := mustPlan(t, e, "")

	for _, it := range claude.items[adapter.ScopeUser] {
		if it.ID == serverID {
			got = it.MCP.Env["EXAMPLE_TOKEN"]
		}
	}
	if got.Literal != resolved {
		t.Error("secret not resolved before export")
	}
	c, found := changeFor(p, at(env, ".example/servers.json"))
	if !found || len(c.Units) != 1 || !c.Units[0].Secret {
		t.Errorf("secret unit not marked: %+v", c.Units)
	}
}

func twoItemAdapter(env paths.Env, unsupported ...adapter.Unsupported) *recording {
	return &recording{
		id:   adapter.Codex,
		caps: adapter.Capabilities{Kinds: map[library.Kind]bool{library.KindRule: true}},
		units: func(adapter.Target) []plan.Unit {
			return []plan.Unit{
				fileUnit(env, adapter.Codex, ".example/rule.md", "rule\n", ruleID),
				fileUnit(env, adapter.Codex, ".example/skill.md", "skill\n", skillID),
			}
		},
		unsupported: unsupported,
	}
}

func TestBlockingExportWithholdsUnitsUntilAcknowledged(t *testing.T) {
	env := home(t)
	codex := twoItemAdapter(env, adapter.Unsupported{ItemID: ruleID, Name: "Plain language", Tool: adapter.Codex, Reason: "cannot be expressed", Blocking: true})
	e := newEngine(env, adapter.Codex)
	e.Adapters = []adapter.Adapter{codex}

	p := mustPlan(t, e, "")

	if _, found := changeFor(p, at(env, ".example/rule.md")); found {
		t.Error("unit of a blocked item planned")
	}
	if _, found := changeFor(p, at(env, ".example/skill.md")); !found {
		t.Error("unit of another item withheld")
	}
	if len(p.Unsupported) != 1 || !p.Unsupported[0].Blocking {
		t.Errorf("unsupported %+v", p.Unsupported)
	}

	e.Acks.Add(policy.Ack{ItemID: ruleID, Tool: adapter.Codex, Hash: library.ContentHash(itemByID(t, e, ruleID))})
	p = mustPlan(t, e, "")

	if _, found := changeFor(p, at(env, ".example/rule.md")); !found {
		t.Error("acknowledged item withheld")
	}
	if len(p.Unsupported) != 1 || p.Unsupported[0].Blocking || p.Unsupported[0].Reason != "acknowledged: cannot be expressed" {
		t.Errorf("unsupported %+v", p.Unsupported)
	}
}

func TestAckForOtherContentDoesNotUnblock(t *testing.T) {
	env := home(t)
	codex := twoItemAdapter(env, adapter.Unsupported{ItemID: ruleID, Tool: adapter.Codex, Reason: "cannot be expressed", Blocking: true})
	e := newEngine(env, adapter.Codex)
	e.Adapters = []adapter.Adapter{codex}
	e.Acks.Add(policy.Ack{ItemID: ruleID, Tool: adapter.Codex, Hash: "sha256:older"})

	p := mustPlan(t, e, "")

	if _, found := changeFor(p, at(env, ".example/rule.md")); found {
		t.Error("stale acknowledgement unblocked the item")
	}
}

func TestNonBlockingUnsupportedWithholdsNothing(t *testing.T) {
	env := home(t)
	codex := twoItemAdapter(env, adapter.Unsupported{ItemID: ruleID, Tool: adapter.Codex, Reason: "larger than the tool reads"})
	e := newEngine(env, adapter.Codex)
	e.Adapters = []adapter.Adapter{codex}

	p := mustPlan(t, e, "")

	for _, rel := range []string{".example/rule.md", ".example/skill.md"} {
		if _, found := changeFor(p, at(env, rel)); !found {
			t.Errorf("%s withheld", rel)
		}
	}
}

func TestWithheldItemKeepsWrittenEntries(t *testing.T) {
	env := home(t)
	path := at(env, ".example/rule.md")
	writeFile(t, path, "rule\n")
	hash, err := manifest.Hash(manifest.UnitFile, []byte("rule\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := manifest.Manifest{Version: 1}
	m.Put(manifest.Entry{Tool: adapter.Codex, Path: path, Root: env.Home, Unit: manifest.UnitFile, Hash: hash, Items: []library.ID{ruleID}})
	if err := m.Save(env.ManifestPath()); err != nil {
		t.Fatal(err)
	}
	codex := twoItemAdapter(env, adapter.Unsupported{ItemID: ruleID, Tool: adapter.Codex, Reason: "cannot be expressed", Blocking: true})
	e := newEngine(env, adapter.Codex)
	e.Adapters = []adapter.Adapter{codex}

	p := mustPlan(t, e, "")

	if c, found := changeFor(p, path); found {
		t.Errorf("withheld item's file planned: op %s removed %d", c.Op, len(c.Removed))
	}
}

func TestUnsupportedItemWithoutUnitsKeepsWrittenEntries(t *testing.T) {
	env := home(t)
	path := at(env, ".example/rule.md")
	writeFile(t, path, "rule\n")
	hash, err := manifest.Hash(manifest.UnitFile, []byte("rule\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := manifest.Manifest{Version: 1}
	m.Put(manifest.Entry{Tool: adapter.Codex, Path: path, Root: env.Home, Unit: manifest.UnitFile, Hash: hash, Items: []library.ID{ruleID}})
	if err := m.Save(env.ManifestPath()); err != nil {
		t.Fatal(err)
	}
	codex := &recording{
		id:          adapter.Codex,
		units:       func(adapter.Target) []plan.Unit { return nil },
		unsupported: []adapter.Unsupported{{ItemID: ruleID, Tool: adapter.Codex, Reason: "no longer expressible"}},
	}
	e := newEngine(env, adapter.Codex)
	e.Adapters = []adapter.Adapter{codex}

	p := mustPlan(t, e, "")

	if c, found := changeFor(p, path); found {
		t.Errorf("unsupported item's file planned: op %s removed %d", c.Op, len(c.Removed))
	}
}

func TestSharedStateFromBackupSets(t *testing.T) {
	env := home(t)
	path := at(env, ".claude.json")
	claude := &recording{
		id:     adapter.ClaudeCode,
		backup: []plan.SnapshotEntry{{Tool: adapter.ClaudeCode, Path: path, Root: env.Home, Keys: []string{"/mcpServers"}}},
		units: func(adapter.Target) []plan.Unit {
			return []plan.Unit{{Tool: adapter.ClaudeCode, Path: path, Root: env.Home, Kind: manifest.UnitJSON, Key: "/mcpServers/example", Content: []byte(`{"command":"example"}`), Items: []library.ID{serverID}}}
		},
	}
	e := newEngine(env, adapter.ClaudeCode)
	e.Adapters = []adapter.Adapter{claude}

	p := mustPlan(t, e, "")

	if c, found := changeFor(p, path); !found || !c.SharedState {
		t.Errorf("shared state not set: %+v", c)
	}
}

func TestConfiguredAdoptAndReplaceSymlinksReachDiff(t *testing.T) {
	env := home(t)
	adopted := at(env, ".example/adopted.md")
	linked := at(env, ".example/linked.md")
	writeFile(t, adopted, "hand written\n")
	if err := os.Symlink("missing.md", linked); err != nil {
		t.Fatal(err)
	}
	codex := &recording{id: adapter.Codex, units: func(adapter.Target) []plan.Unit {
		return []plan.Unit{
			fileUnit(env, adapter.Codex, ".example/adopted.md", "managed\n", ruleID),
			fileUnit(env, adapter.Codex, ".example/linked.md", "managed\n", ruleID),
		}
	}}
	e := newEngine(env, adapter.Codex)
	e.Adapters = []adapter.Adapter{codex}

	p := mustPlan(t, e, "")
	for _, path := range []string{adopted, linked} {
		if c, _ := changeFor(p, path); c.Blocked == "" {
			t.Errorf("%s not blocked without consent", path)
		}
	}

	e.Config.Adopted = []string{adopted}
	e.Config.ReplaceSymlinks = []string{linked}
	p = mustPlan(t, e, "")
	for _, path := range []string{adopted, linked} {
		if c, _ := changeFor(p, path); c.Blocked != "" {
			t.Errorf("%s blocked: %s", path, c.Blocked)
		}
	}

	e.Config.ReplaceSymlinks = nil
	p, err := e.Plan("", plan.Options{ReplaceSymlinks: []string{linked}})
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := changeFor(p, linked); c.Blocked != "" || !c.ReplaceLink {
		t.Errorf("flag consent ignored: %+v", c)
	}
}

func TestEntriesOfOtherProjectsLeftAlone(t *testing.T) {
	env := home(t)
	other := t.TempDir()
	path := filepath.Join(other, "AGENTS.md")
	writeFile(t, path, "# Other\n")
	m := manifest.Manifest{Version: 1}
	m.Put(manifest.Entry{Tool: adapter.AgentsMD + "," + adapter.Codex, Path: path, Root: other, Unit: manifest.UnitSection, Hash: "sha256:x", Items: []library.ID{ruleID}})
	m.Put(manifest.Entry{Tool: adapter.Codex, Path: filepath.Join(other, ".codex", "config.toml"), Root: other, Unit: manifest.UnitTOMLTable, Key: "mcp_servers.example", Hash: "sha256:x", Items: []library.ID{serverID}})
	if err := m.Save(env.ManifestPath()); err != nil {
		t.Fatal(err)
	}

	for _, project := range []string{"", t.TempDir()} {
		p := mustPlan(t, newEngine(env, adapter.Codex), project)
		for _, c := range p.Changes {
			if strings.HasPrefix(c.Path, other) {
				t.Errorf("project %q planned %s in another project", project, c.Path)
			}
		}
	}
}

func TestPlanReportsLibraryHash(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.Codex)
	merged, _, err := e.Library("")
	if err != nil {
		t.Fatal(err)
	}
	if p := mustPlan(t, e, ""); p.LibraryHash != merged.Hash() || p.LibraryHash == "" {
		t.Errorf("hash %q", p.LibraryHash)
	}
}

func TestSyncThenRestoreReturnsOriginalTree(t *testing.T) {
	env := home(t)
	before := testenv.TreeHash(t, env.Home)
	e := newEngine(env, adapter.ClaudeCode, adapter.Codex)
	e.Acks.Add(policy.Ack{ItemID: permissionID, Tool: adapter.Codex, Hash: library.ContentHash(itemByID(t, e, permissionID))})

	result, p, err := e.Sync("", plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.SnapshotID == "" || len(result.Written) == 0 {
		t.Fatalf("nothing written: %+v", result)
	}
	for _, c := range p.Changes {
		if c.Blocked != "" {
			t.Errorf("%s blocked: %s", c.Path, c.Blocked)
		}
	}
	for _, rel := range []string{".claude/skills/release-notes/SKILL.md", ".agents/skills/release-notes/SKILL.md", ".claude.json", ".codex/config.toml"} {
		if !slices.Contains(result.Written, at(env, rel)) {
			t.Errorf("%s not written: %v", rel, result.Written)
		}
	}
	if testenv.TreeHash(t, env.Home) == before {
		t.Fatal("sync changed nothing")
	}

	restored, err := e.Backups().Restore(result.SnapshotID, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range restored.Actions {
		if a.Op == "blocked" {
			t.Errorf("restore blocked %s: %s", a.Path, a.Detail)
		}
	}
	if got := testenv.TreeHash(t, env.Home); got != before {
		t.Error("home differs from the original tree after restore")
	}
}

func TestSkillWrittenToEveryHomeIdentically(t *testing.T) {
	env := home(t)
	project := t.TempDir()
	e := newEngine(env, allTools...)
	for _, tool := range allTools {
		e.Acks.Add(policy.Ack{ItemID: permissionID, Tool: tool, Hash: library.ContentHash(itemByID(t, e, permissionID))})
	}

	p := mustPlan(t, e, project)

	homes := []string{
		at(env, ".claude/skills/release-notes/SKILL.md"),
		at(env, ".agents/skills/release-notes/SKILL.md"),
		at(env, ".gemini/config/skills/release-notes/SKILL.md"),
		filepath.Join(project, ".agents/skills/release-notes/SKILL.md"),
		filepath.Join(project, ".claude/skills/release-notes/SKILL.md"),
	}
	var first []byte
	for _, path := range homes {
		c, found := changeFor(p, path)
		if !found || c.Blocked != "" || c.Op != plan.OpCreate {
			t.Errorf("%s found %v blocked %q op %s", path, found, c.Blocked, c.Op)
			continue
		}
		if first == nil {
			first = c.After
		}
		if !bytes.Equal(c.After, first) {
			t.Errorf("%s differs:\n%s", path, c.After)
		}
	}
	for _, c := range p.Changes {
		if strings.Contains(c.Path, "release-notes") && !slices.Contains(homes, c.Path) {
			t.Errorf("skill also written to %s", c.Path)
		}
	}
}

func TestResolveSecretReturnsWrittenValue(t *testing.T) {
	env := home(t)
	writeFile(t, filepath.Join(env.LibraryDir(), "mcp", "example-docs.yaml"), "promptstor:\n  id: "+string(serverID)+"\ntransport: http\nurl: https://mcp.example.com/v1\nheaders:\n  Authorization:\n    ref: keychain:example-service/example-account\n")
	e := newEngine(env, adapter.ClaudeCode)
	e.Config.ResolveSecrets = []string{"example-docs"}
	e.Resolver = resolver{"keychain:example-service/example-account": resolved}
	e.Acks.Add(policy.Ack{ItemID: permissionID, Tool: adapter.ClaudeCode, Hash: library.ContentHash(itemByID(t, e, permissionID))})
	if _, _, err := e.Sync("", plan.Options{}); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(env.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	var secret manifest.Entry
	for _, entry := range m.Entries {
		if entry.Secret {
			secret = entry
		}
	}
	if secret.Path != at(env, ".claude.json") {
		t.Fatalf("no secret entry in %+v", m.Entries)
	}

	value, err := e.ResolveSecret(adapter.ClaudeCode, secret.Path, secret.Key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(value, []byte(resolved)) {
		t.Error("resolved value missing")
	}

	e.Resolver = resolver{}
	if _, err := e.ResolveSecret(adapter.ClaudeCode, secret.Path, secret.Key); err == nil {
		t.Error("resolver failure not reported")
	}
}

func TestSyncAfterRestoreWritesAgain(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode, adapter.Codex)
	e.Acks.Add(policy.Ack{ItemID: permissionID, Tool: adapter.Codex, Hash: library.ContentHash(itemByID(t, e, permissionID))})
	first, _, err := e.Sync("", plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	synced := testenv.TreeHash(t, env.Home)
	if _, err := e.Backups().Restore(first.SnapshotID, "", false); err != nil {
		t.Fatal(err)
	}

	_, p, err := e.Sync("", plan.Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range p.Changes {
		if c.Blocked != "" {
			t.Errorf("%s blocked: %s", c.Path, c.Blocked)
		}
	}
	if testenv.TreeHash(t, env.Home) != synced {
		t.Error("second sync did not reproduce the first")
	}
}

func TestRestoreReconcilesManifest(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode, adapter.Codex)
	e.Acks.Add(policy.Ack{ItemID: permissionID, Tool: adapter.Codex, Hash: library.ContentHash(itemByID(t, e, permissionID))})
	first, _, err := e.Sync("", plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	synced := testenv.TreeHash(t, env.Home)
	before, err := manifest.Load(env.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}

	restored, err := e.Restore(first.SnapshotID, adapter.Codex, false)
	if err != nil {
		t.Fatal(err)
	}

	after, err := manifest.Load(env.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	restoredPaths := map[string]bool{}
	for _, f := range restored.Snapshot.Files {
		if slices.Contains(strings.Split(f.Tool, ","), adapter.Codex) {
			restoredPaths[f.Path] = true
		}
	}
	for _, entry := range after.Entries {
		if restoredPaths[entry.Path] {
			t.Errorf("entry for restored %s %s kept", entry.Path, entry.Key)
		}
	}
	for _, entry := range before.Entries {
		if !restoredPaths[entry.Path] && !slices.ContainsFunc(after.Entries, func(a manifest.Entry) bool { return a.Path == entry.Path && a.Key == entry.Key }) {
			t.Errorf("entry for %s %s dropped although its file was not restored", entry.Path, entry.Key)
		}
	}

	p := mustPlan(t, e, "")
	for _, c := range p.Changes {
		if len(c.Removed) > 0 || c.Blocked != "" {
			t.Errorf("%s: removed %d blocked %q", c.Path, len(c.Removed), c.Blocked)
		}
	}
	_, p, err = e.Sync("", plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Changes {
		if c.Blocked != "" {
			t.Errorf("%s blocked: %s", c.Path, c.Blocked)
		}
	}
	if testenv.TreeHash(t, env.Home) != synced {
		t.Error("second sync did not write the restored files again")
	}
}

func TestDryRunRestoreLeavesManifest(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode)
	first, _, err := e.Sync("", plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	before := testenv.TreeHash(t, env.StateHome)

	if _, err := e.Restore(first.SnapshotID, "", true); err != nil {
		t.Fatal(err)
	}

	if testenv.TreeHash(t, env.StateHome) != before {
		t.Error("dry run changed the state directory")
	}
}

func TestEditGuardsInstallAtUserScopeOnly(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode)
	e.Builtins = []library.Item{library.EditGuard(adapter.ClaudeCode, "/opt/promptstor/bin/promptstor")}
	project := t.TempDir()
	p := mustPlan(t, e, project)
	user, found := changeFor(p, at(env, ".claude/settings.json"))
	if !found || !bytes.Contains(user.After, []byte("promptstor hook claude-code")) {
		t.Fatalf("user settings change %+v, want the guard hook", user)
	}
	for _, c := range p.Changes {
		if strings.HasPrefix(c.Path, project) && bytes.Contains(c.After, []byte("promptstor hook")) {
			t.Errorf("project file %s gets the edit guard", c.Path)
		}
	}
}

func TestPromptstorMCPRegistersAtUserScopeOnly(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode)
	e.Builtins = []library.Item{library.PromptstorMCP("/opt/promptstor/bin/promptstor")}
	project := t.TempDir()
	p := mustPlan(t, e, project)
	user, found := changeFor(p, at(env, ".claude.json"))
	if !found || !bytes.Contains(user.After, []byte(`"/opt/promptstor/bin/promptstor"`)) {
		t.Fatalf("user change %+v, want the promptstor server", user)
	}
	for _, c := range p.Changes {
		if strings.HasPrefix(c.Path, project) && bytes.Contains(c.After, []byte("/opt/promptstor/bin/promptstor")) {
			t.Errorf("project file %s registers the promptstor server", c.Path)
		}
	}
}
