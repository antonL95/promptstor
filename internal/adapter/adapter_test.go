package adapter_test

import (
	"bytes"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/adapter/fake"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/testenv"
)

func skill(t *testing.T) library.Item {
	t.Helper()
	items, err := library.LoadLayer("testdata/layer", library.LayerPersonal, library.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != library.KindSkill {
		t.Fatalf("items %+v", items)
	}
	return items[0]
}

func target(t *testing.T, scope adapter.Scope) adapter.Target {
	t.Helper()
	env := paths.Env{Home: "/home/user", ConfigHome: "/home/user/.config", StateHome: "/home/user/.local/state", OS: "linux"}
	return adapter.Target{Env: env, Scope: scope, ProjectRoot: "/work/app"}
}

func unitsFor(t *testing.T, tg adapter.Target, it library.Item, tools ...adapter.ToolID) []plan.Unit {
	t.Helper()
	var units []plan.Unit
	for _, tool := range tools {
		got, err := adapter.SkillUnits(tg, tool, it)
		if err != nil {
			t.Fatal(err)
		}
		units = append(units, got...)
	}
	return units
}

func skillDirs(units []plan.Unit) []string {
	var dirs []string
	for _, u := range units {
		if filepath.Base(u.Path) == "SKILL.md" {
			dirs = append(dirs, filepath.Dir(u.Path))
		}
	}
	slices.Sort(dirs)
	return slices.Compact(dirs)
}

func TestSkillUnitsPerSelection(t *testing.T) {
	it := skill(t)
	user := target(t, adapter.ScopeUser)
	project := target(t, adapter.ScopeProject)
	cases := []struct {
		name  string
		tg    adapter.Target
		tools []adapter.ToolID
		want  []string
	}{
		{"claude and codex", user, []adapter.ToolID{adapter.ClaudeCode, adapter.Codex}, []string{"/home/user/.agents/skills/release-notes", "/home/user/.claude/skills/release-notes"}},
		{"antigravity alone", user, []adapter.ToolID{adapter.Antigravity}, []string{"/home/user/.gemini/config/skills/release-notes"}},
		{"agents home readers", user, []adapter.ToolID{adapter.Codex, adapter.GeminiCLI, adapter.OpenCode, adapter.Cursor}, []string{"/home/user/.agents/skills/release-notes"}},
		{"project claude", project, []adapter.ToolID{adapter.ClaudeCode}, []string{"/work/app/.claude/skills/release-notes"}},
		{"project others", project, []adapter.ToolID{adapter.Codex, adapter.Antigravity, adapter.Cursor}, []string{"/work/app/.agents/skills/release-notes"}},
		{"agents-md writes no skill", user, []adapter.ToolID{adapter.AgentsMD}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := skillDirs(unitsFor(t, c.tg, it, c.tools...)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("homes %v, want %v", got, c.want)
			}
		})
	}
}

func TestSkillUnitsShape(t *testing.T) {
	it := skill(t)
	units := unitsFor(t, target(t, adapter.ScopeUser), it, adapter.Codex)
	root := "/home/user"
	dir := "/home/user/.agents/skills/release-notes"
	want := map[string]fs.FileMode{
		dir + "/SKILL.md":            0o644,
		dir + "/references/style.md": 0o644,
		dir + "/scripts/run.sh":      0o755,
	}
	if len(units) != len(want) {
		t.Fatalf("units %+v", units)
	}
	for i, u := range units {
		if i > 0 && units[i-1].Path >= u.Path {
			t.Errorf("units not sorted at %s", u.Path)
		}
		mode, ok := want[u.Path]
		if !ok || u.Mode != mode || u.Root != root || u.Tool != adapter.Codex || u.Kind != manifest.UnitFile || u.Key != "" || u.Secret {
			t.Errorf("unit %+v", u)
		}
		if !reflect.DeepEqual(u.Items, []library.ID{it.ID}) {
			t.Errorf("items %v", u.Items)
		}
		if _, err := manifest.ParseKey(u.Kind, u.Key); err != nil {
			t.Error(err)
		}
	}
	main := units[0]
	if !bytes.Contains(main.Content, []byte(managed.Notice)) {
		t.Error("SKILL.md has no notice")
	}
	if !strings.HasPrefix(string(main.Content), "---\nname: release-notes\ndescription: Draft release notes from the commit log\nallowed-tools:") {
		t.Errorf("frontmatter order: %q", main.Content)
	}
	testenv.Golden(t, "testdata/golden/SKILL.md", main.Content)
	if got := string(units[2].Content); got != string(it.Files["scripts/run.sh"].Data) {
		t.Errorf("attachment content %q", got)
	}
}

func TestSkillUnitsIdenticalAcrossTools(t *testing.T) {
	it := skill(t)
	tg := target(t, adapter.ScopeProject)
	first := unitsFor(t, tg, it, adapter.Codex)
	for _, tool := range []adapter.ToolID{adapter.GeminiCLI, adapter.OpenCode, adapter.Cursor, adapter.Antigravity} {
		other := unitsFor(t, tg, it, tool)
		for i := range other {
			other[i].Tool = adapter.Codex
		}
		if !reflect.DeepEqual(first, other) {
			t.Errorf("%s units differ from codex", tool)
		}
	}
}

func TestSkillUnitsRejectInvalidSkills(t *testing.T) {
	cases := map[string]func(*library.Item){
		"name with separator":   func(it *library.Item) { it.Name = "a/b" },
		"name leaving the home": func(it *library.Item) { it.Name = ".." },
		"empty name":            func(it *library.Item) { it.Name = "" },
		"attachment outside":    func(it *library.Item) { it.Files["../escape.sh"] = library.File{Data: []byte("x")} },
		"attachment SKILL.md":   func(it *library.Item) { it.Files["SKILL.md"] = library.File{Data: []byte("x")} },
		"meta not encodable":    func(it *library.Item) { it.Meta["bad"] = make(chan int) },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			it := skill(t)
			breakIt(&it)
			units, err := adapter.SkillUnits(target(t, adapter.ScopeUser), adapter.Codex, it)
			if err == nil || units != nil {
				t.Errorf("units %v, error %v", units, err)
			}
		})
	}
	if units, err := adapter.SkillUnits(target(t, adapter.ScopeUser), adapter.AgentsMD, skill(t)); err != nil || units != nil {
		t.Errorf("agents-md: units %v, error %v", units, err)
	}
}

func TestSkillReaders(t *testing.T) {
	cases := []struct {
		scope adapter.Scope
		dir   string
		want  []adapter.ToolID
	}{
		{adapter.ScopeUser, "~/.agents/skills", []adapter.ToolID{adapter.Codex, adapter.Cursor, adapter.GeminiCLI, adapter.OpenCode}},
		{adapter.ScopeUser, "~/.claude/skills", []adapter.ToolID{adapter.ClaudeCode, adapter.Cursor, adapter.OpenCode}},
		{adapter.ScopeUser, "~/.gemini/config/skills", []adapter.ToolID{adapter.Antigravity}},
		{adapter.ScopeProject, ".agents/skills", []adapter.ToolID{adapter.Antigravity, adapter.Codex, adapter.Cursor, adapter.GeminiCLI, adapter.OpenCode}},
		{adapter.ScopeProject, ".claude/skills", []adapter.ToolID{adapter.ClaudeCode, adapter.Cursor, adapter.OpenCode}},
		{adapter.ScopeUser, ".agents/skills", nil},
		{adapter.ScopeProject, "~/.agents/skills", nil},
		{adapter.ScopeUser, "~/.codex/skills", nil},
	}
	for _, c := range cases {
		if got := adapter.SkillReaders(c.scope, c.dir); !reflect.DeepEqual(got, c.want) {
			t.Errorf("SkillReaders(%s, %s) = %v, want %v", c.scope, c.dir, got, c.want)
		}
	}
}

func TestElementKeyRoundTrips(t *testing.T) {
	good := []struct{ pointer, sub, value, want string }{
		{"/permissions/allow", "", "Bash(pwd)", "/permissions/allow@=Bash(pwd)"},
		{"/hooks/PreToolUse", "/matcher", "Bash", "/hooks/PreToolUse@/matcher=Bash"},
		{"/hooks/Stop", "/matcher", "", "/hooks/Stop@/matcher="},
		{"/list", "", "a=b@c", "/list@=a=b@c"},
		{"/list", "/name", "x@y=z", "/list@/name=x@y=z"},
		{"/a~1b", "/c~0d", "v", "/a~1b@/c~0d=v"},
	}
	for _, c := range good {
		got, err := adapter.ElementKey(c.pointer, c.sub, c.value)
		if err != nil || got != c.want {
			t.Errorf("ElementKey(%q, %q, %q) = %q, %v; want %q", c.pointer, c.sub, c.value, got, err, c.want)
		}
	}
	bad := []struct{ pointer, sub, value string }{
		{"/a@b", "", "x"},
		{"/a@b", "/name", "x"},
		{"/a", "/na=me", "x"},
		{"/a", "name", "x"},
		{"a", "", "x"},
		{"/a~2", "", "x"},
		{"/a", "/b~", "x"},
	}
	for _, c := range bad {
		if got, err := adapter.ElementKey(c.pointer, c.sub, c.value); err == nil {
			t.Errorf("ElementKey(%q, %q, %q) = %q, want an error", c.pointer, c.sub, c.value, got)
		}
	}
}

func TestRegistry(t *testing.T) {
	codex := &fake.Adapter{Tool: adapter.Codex}
	claude := &fake.Adapter{Tool: adapter.ClaudeCode}
	adapter.Register(codex)
	adapter.Register(claude)
	if got, ok := adapter.Get(adapter.Codex); !ok || got != codex {
		t.Errorf("Get codex = %v %v", got, ok)
	}
	if _, ok := adapter.Get("unknown"); ok {
		t.Error("unknown tool found")
	}
	var ids []adapter.ToolID
	for _, a := range adapter.All() {
		ids = append(ids, a.ID())
	}
	if !slices.IsSorted(ids) || !slices.Contains(ids, adapter.Codex) || !slices.Contains(ids, adapter.ClaudeCode) {
		t.Errorf("All = %v", ids)
	}
	replacement := &fake.Adapter{Tool: adapter.Codex}
	adapter.Register(replacement)
	if got, _ := adapter.Get(adapter.Codex); got != replacement {
		t.Error("Register did not replace the adapter")
	}
}

func TestFakeAdapterExportsCopies(t *testing.T) {
	unit := plan.Unit{Tool: adapter.Codex, Path: "/home/user/x", Root: "/home/user", Kind: manifest.UnitFile, Content: []byte("x\n")}
	f := &fake.Adapter{
		Tool:        adapter.Codex,
		Units:       []plan.Unit{unit},
		Unsupported: []adapter.Unsupported{{ItemID: "a", Tool: adapter.Codex, Reason: "no hooks"}},
	}
	units, unsupported, err := f.Export(target(t, adapter.ScopeUser), nil)
	if err != nil || len(units) != 1 || len(unsupported) != 1 {
		t.Fatalf("export %v %v %v", units, unsupported, err)
	}
	units[0].Content[0] = 'y'
	if string(f.Units[0].Content) != "x\n" {
		t.Error("export shares unit content with the fake")
	}
}
