package agentsmd_test

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
	"github.com/antonL95/promptstor/internal/adapter/agentsmd"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	notScoped = "file scope not enforced; the rule applies everywhere"
	notAgent  = "agent activation not supported; the rule always applies"
)

func registered(t *testing.T) adapter.Adapter {
	t.Helper()
	a, ok := adapter.Get(adapter.AgentsMD)
	if !ok {
		t.Fatal("agents-md is not registered")
	}
	return a
}

func target(t *testing.T, scope adapter.Scope, selected ...adapter.ToolID) adapter.Target {
	t.Helper()
	return adapter.Target{
		Env:         testenv.Home(t, "testdata/basic/home"),
		Scope:       scope,
		ProjectRoot: testenv.Home(t, "testdata/basic/project").Home,
		Selected:    selected,
	}
}

func rules() []library.Item {
	return []library.Item{
		{ID: "ps_build", Kind: library.KindRule, Name: "Build", Body: "## Build\n\nRun `make build` before a commit.\n"},
		{ID: "ps_go", Kind: library.KindRule, Name: "Go style", Activation: library.ActivationFiles, Scope: []string{"**/*.go", "go.mod"}, Body: "## Go style\n\nUse tabs.\n"},
		{ID: "ps_claude", Kind: library.KindRule, Name: "Claude only", Targets: library.Targets{Include: []string{adapter.ClaudeCode}}, Body: "## Claude only\n\nPlan first.\n"},
		{ID: "ps_release", Kind: library.KindRule, Name: "Releases", Activation: library.ActivationAgent, Description: "Preparing\na release", Body: "## Releases\n\nUpdate the changelog.\n"},
	}
}

func export(t *testing.T, tg adapter.Target, items []library.Item) ([]plan.Unit, []adapter.Unsupported) {
	t.Helper()
	units, unsupported, err := registered(t).Export(tg, items)
	if err != nil {
		t.Fatal(err)
	}
	return units, unsupported
}

func describe(root string, units []plan.Unit) string {
	var out strings.Builder
	for _, u := range units {
		var items []string
		for _, id := range u.Items {
			items = append(items, string(id))
		}
		fmt.Fprintf(&out, "tool=%s path=%s root=%s kind=%s key=%q mode=%#o secret=%v items=%s\n",
			u.Tool, strings.Replace(u.Path, root, "<project>", 1), strings.Replace(u.Root, root, "<project>", 1),
			u.Kind, u.Key, u.Mode, u.Secret, strings.Join(items, ","))
	}
	return out.String()
}

func itemsYAML(t *testing.T, root string, items []library.Item) []byte {
	t.Helper()
	type view struct {
		Kind   library.Kind `yaml:"kind"`
		Name   string       `yaml:"name"`
		Order  int          `yaml:"order"`
		Source string       `yaml:"source"`
		Body   string       `yaml:"body"`
	}
	views := []view{}
	for _, it := range items {
		views = append(views, view{it.Kind, it.Name, it.Order, strings.Replace(it.Source, root, "<project>", 1), it.Body})
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	encoder.CompactSeqIndent()
	if err := encoder.Encode(views); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func importFrom(t *testing.T, tg adapter.Target) adapter.Imported {
	t.Helper()
	imported, err := registered(t).Import(tg)
	if err != nil {
		t.Fatal(err)
	}
	return imported
}

func TestDetect(t *testing.T) {
	a := registered(t)
	if a.ID() != adapter.AgentsMD {
		t.Errorf("ID %q", a.ID())
	}
	got := a.Detect(testenv.Home(t, "testdata/basic/home"))
	if got.Installed || len(got.Paths) != 0 {
		t.Errorf("detection %+v", got)
	}
}

func TestCapabilities(t *testing.T) {
	caps := registered(t).Capabilities()
	var kinds, activations []string
	for _, kind := range slices.Sorted(maps.Keys(caps.Kinds)) {
		if caps.Kinds[kind] {
			kinds = append(kinds, string(kind))
		}
	}
	for _, activation := range slices.Sorted(maps.Keys(caps.Activations)) {
		activations = append(activations, string(activation))
	}
	table := fmt.Sprintf("kinds: %s\nfile scope: %v\nactivations: %s\nportable: deny-commands=%v deny-paths=%v allow-mcp-servers=%v\nreads project AGENTS.md: %v\nskill homes: %d\ncommands as skills: %v\nenv ref: %v\n",
		strings.Join(kinds, ","), caps.FileScope, strings.Join(activations, ","),
		caps.Portable.DenyCommands, caps.Portable.DenyPaths, caps.Portable.AllowMCPServers,
		caps.ReadsProjectAgentsMD, len(caps.SkillHomes), caps.CommandsAsSkills, caps.EnvRef != nil)
	testenv.Golden(t, "testdata/golden/capabilities.txt", []byte(table))
}

func TestImportUserScope(t *testing.T) {
	tg := target(t, adapter.ScopeUser, adapter.Codex)
	imported := importFrom(t, tg)
	testenv.Golden(t, "testdata/golden/import-user.yaml", itemsYAML(t, tg.ProjectRoot, imported.Items))
	if set := registered(t).BackupSet(tg); len(set) != 0 {
		t.Errorf("user backup set %+v", set)
	}
}

func TestImportProjectScope(t *testing.T) {
	tg := target(t, adapter.ScopeProject, adapter.Codex)
	imported := importFrom(t, tg)
	testenv.Golden(t, "testdata/golden/import-project.yaml", itemsYAML(t, tg.ProjectRoot, imported.Items))
	path := filepath.Join(tg.ProjectRoot, "AGENTS.md")
	want := []plan.SnapshotEntry{{Tool: adapter.AgentsMD, Path: path, Root: tg.ProjectRoot}}
	if got := registered(t).BackupSet(tg); !reflect.DeepEqual(got, want) {
		t.Errorf("backup set %+v", got)
	}
}

func TestImportMissingFile(t *testing.T) {
	tg := target(t, adapter.ScopeProject, adapter.Codex)
	tg.ProjectRoot = t.TempDir()
	if got := importFrom(t, tg); !reflect.DeepEqual(got, adapter.Imported{}) {
		t.Errorf("imported %+v", got)
	}
}

func TestImportReportsSymlinkWithoutFollowing(t *testing.T) {
	tg := target(t, adapter.ScopeProject, adapter.Codex)
	tg.ProjectRoot = t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("## Outside\n\nNever read.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tg.ProjectRoot, "AGENTS.md")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	want := adapter.Imported{Notes: []adapter.Note{{Path: path, Message: "symlinked -> " + outside + "; not imported"}}}
	if got := importFrom(t, tg); !reflect.DeepEqual(got, want) {
		t.Errorf("imported %+v", got)
	}
}

func TestImportUnreadableFileNamesIt(t *testing.T) {
	tg := target(t, adapter.ScopeProject, adapter.Codex)
	tg.ProjectRoot = t.TempDir()
	path := filepath.Join(tg.ProjectRoot, "AGENTS.md")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := registered(t).Import(tg); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("error %v", err)
	}
}

func TestSecretsScrubbedOnImport(t *testing.T) {
	tg := target(t, adapter.ScopeProject, adapter.Codex)
	imported := importFrom(t, tg)
	if len(imported.Items) == 0 {
		t.Fatal("nothing imported")
	}
	for _, it := range imported.Items {
		if it.Kind != library.KindRule || it.MCP != nil || strings.Contains(it.Body, "fixture-token-value") {
			t.Errorf("item %s %q carries data from another file", it.Kind, it.Name)
		}
	}
}

func TestExportUserScope(t *testing.T) {
	tg := target(t, adapter.ScopeUser, adapter.ClaudeCode, adapter.Codex)
	userUnits, userUnsupported := export(t, tg, rules())
	if len(userUnsupported) != 0 {
		t.Errorf("unsupported %+v", userUnsupported)
	}
	tg.Scope = adapter.ScopeProject
	projectUnits, _ := export(t, tg, rules())
	got := fmt.Sprintf("user scope: %d units\n%sproject scope: %d units\n%s",
		len(userUnits), describe(tg.ProjectRoot, userUnits), len(projectUnits), describe(tg.ProjectRoot, projectUnits))
	testenv.Golden(t, "testdata/golden/units.txt", []byte(got))
}

func TestExportProjectScope(t *testing.T) {
	tg := target(t, adapter.ScopeProject, adapter.ClaudeCode, adapter.Codex)
	units, _ := export(t, tg, rules()[2:3])
	if len(units) != 0 {
		t.Errorf("units for a claude only rule %+v", units)
	}
}

func TestExportGolden(t *testing.T) {
	tg := target(t, adapter.ScopeProject, adapter.ClaudeCode, adapter.Codex, adapter.AgentsMD)
	tg.ProjectRoot = t.TempDir()
	items := rules()
	units, _ := export(t, tg, items)
	changes, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.AgentsMD}, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Blocked != "" || changes[0].Op != plan.OpCreate {
		t.Fatalf("changes %+v", changes)
	}
	testenv.Golden(t, "testdata/golden/AGENTS.md", changes[0].After)
	if !reflect.DeepEqual(items, rules()) {
		t.Error("export changed the items it was given")
	}
}

func TestScopedRuleAnnotated(t *testing.T) {
	tg := target(t, adapter.ScopeProject, adapter.Codex, adapter.Cursor, adapter.ClaudeCode)
	_, unsupported := export(t, tg, rules())
	want := []adapter.Unsupported{
		{ItemID: "ps_go", Name: "Go style", Tool: adapter.Codex, Reason: notScoped},
		{ItemID: "ps_release", Name: "Releases", Tool: adapter.ClaudeCode, Reason: notAgent},
		{ItemID: "ps_release", Name: "Releases", Tool: adapter.Codex, Reason: notAgent},
	}
	if !reflect.DeepEqual(unsupported, want) {
		t.Errorf("unsupported %+v", unsupported)
	}

	cases := []struct {
		name string
		item library.Item
		want string
	}{
		{
			"text directly after the heading",
			library.Item{Scope: []string{"*.go"}, Body: "## A\nText.\n"},
			"## A\nApplies to: `*.go`\n\nText.\n",
		},
		{
			"no heading",
			library.Item{Scope: []string{"*.go"}, Body: "Text.\n"},
			"Applies to: `*.go`\n\nText.\n",
		},
		{
			"heading only",
			library.Item{Scope: []string{"*.go"}, Body: "## A\n"},
			"## A\nApplies to: `*.go`\n",
		},
		{
			"stale annotation replaced",
			library.Item{Scope: []string{"*.go"}, Body: "## A\nApplies to: `*.ts`, `*.js`\n\nText.\n"},
			"## A\nApplies to: `*.go`\n\nText.\n",
		},
		{
			"user prose kept",
			library.Item{Scope: []string{"*.go"}, Body: "## A\nApplies to: `main` only on release days.\n"},
			"## A\nApplies to: `*.go`\n\nApplies to: `main` only on release days.\n",
		},
		{
			"agent without description",
			library.Item{Activation: library.ActivationAgent, Body: "## A\n\nText.\n"},
			"## A\n\nText.\n",
		},
		{
			"files without globs",
			library.Item{Activation: library.ActivationFiles, Body: "## A\n\nText.\n"},
			"## A\n\nText.\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.item.Kind = library.KindRule
			units, _ := export(t, tg, []library.Item{c.item})
			if len(units) != 1 || string(units[0].Content) != c.want {
				t.Errorf("units %+v, want %q", units, c.want)
			}
		})
	}
}

func TestUnsupportedReported(t *testing.T) {
	tg := target(t, adapter.ScopeProject, adapter.Codex)
	var items []library.Item
	var want []adapter.Unsupported
	for _, kind := range []library.Kind{library.KindSkill, library.KindCommand, library.KindSubagent, library.KindPermission, library.KindHook, library.KindMCP} {
		id := library.ID("ps_" + string(kind))
		items = append(items, library.Item{ID: id, Kind: kind, Name: string(kind)})
		want = append(want, adapter.Unsupported{ItemID: id, Name: string(kind), Tool: adapter.AgentsMD, Reason: "AGENTS.md holds rules only"})
	}
	units, unsupported := export(t, tg, items)
	if len(units) != 0 || !reflect.DeepEqual(unsupported, want) {
		t.Errorf("units %+v, unsupported %+v", units, unsupported)
	}
}

func TestRoundTripNoChanges(t *testing.T) {
	tg := target(t, adapter.ScopeProject, adapter.ClaudeCode, adapter.Codex)
	before := testenv.TreeHash(t, tg.ProjectRoot)
	imported := importFrom(t, tg)
	units, unsupported := export(t, tg, imported.Items)
	if len(unsupported) != 0 {
		t.Errorf("unsupported %+v", unsupported)
	}
	changes, err := plan.Diff(units, manifest.Manifest{}, []string{adapter.AgentsMD}, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes %+v", changes)
	}
	for _, change := range changes {
		if change.Op != plan.OpNoop || change.Blocked != "" {
			t.Errorf("%s %s %q: %s", change.Op, change.Path, change.Blocked, change.After)
		}
	}
	if testenv.TreeHash(t, tg.ProjectRoot) != before {
		t.Error("the project changed")
	}
	if !agentsmd.NeededFor(tg.Selected) {
		t.Error("AGENTS.md not needed")
	}
}
