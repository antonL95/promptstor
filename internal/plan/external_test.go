package plan_test

import (
	"testing"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/testenv"
)

func TestDiffBlocksExternalPaths(t *testing.T) {
	env := home(t)
	file := unit(env, adapter.Codex, ".codex/AGENTS.md", manifest.UnitSection, "", "Use tabs.\n")
	inDir := unit(env, adapter.ClaudeCode, ".claude/skills/x/SKILL.md", manifest.UnitFile, "", "skill\n")
	sibling := unit(env, adapter.ClaudeCode, ".claude/skills-extra/y/SKILL.md", manifest.UnitFile, "", "other\n")
	external := []string{file.Path, at(env, ".claude/skills") + "/"}

	changes := diff(t, []plan.Unit{file, inDir, sibling}, manifest.Manifest{}, []string{adapter.Codex, adapter.ClaudeCode}, plan.Options{External: external})

	if c := only(t, changes, file.Path); c.Blocked != "managed externally: "+file.Path || c.Op != plan.OpNoop {
		t.Errorf("file: blocked %q op %s", c.Blocked, c.Op)
	}
	if c := only(t, changes, inDir.Path); c.Blocked != "managed externally: "+at(env, ".claude/skills") || c.Op != plan.OpNoop {
		t.Errorf("directory: blocked %q op %s", c.Blocked, c.Op)
	}
	if c := only(t, changes, sibling.Path); c.Blocked != "" || c.Op != plan.OpCreate {
		t.Errorf("sibling: blocked %q op %s", c.Blocked, c.Op)
	}
	before := testenv.TreeHash(t, env.Home)
	snap := &recorder{t: t, root: env.Home}
	var m manifest.Manifest
	blockedOnly := []plan.FileChange{only(t, changes, file.Path), only(t, changes, inDir.Path)}
	if _, err := apply(env, blockedOnly, snap, &m); err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, env, before, snap)
}

func TestExternalEntriesAreKept(t *testing.T) {
	env := home(t)
	write(t, at(env, ".claude/skills/x/SKILL.md"), "skill\n")
	held := unit(env, adapter.ClaudeCode, ".claude/skills/x/SKILL.md", manifest.UnitFile, "", "skill\n")
	dropped := unit(env, adapter.ClaudeCode, ".claude/settings.json", manifest.UnitJSON, "/model", `"example-model"`)
	var m manifest.Manifest
	m.Put(entryFor(t, held))
	m.Put(entryFor(t, dropped))

	changes := diff(t, nil, m, []string{adapter.ClaudeCode}, plan.Options{External: []string{at(env, ".claude/skills")}})

	for _, c := range changes {
		if c.Path == held.Path {
			t.Errorf("external entry planned: op %s removed %d blocked %q", c.Op, len(c.Removed), c.Blocked)
		}
	}
	if c := only(t, changes, dropped.Path); len(c.Removed) != 1 {
		t.Errorf("dropped entry not removed: %+v", c.Removed)
	}
}

func TestExternalFor(t *testing.T) {
	cases := []struct {
		name, path, want string
		found            bool
	}{
		{"same file", "/h/.codex/AGENTS.md", "/h/.codex/AGENTS.md", true},
		{"below directory", "/h/.claude/skills/x/SKILL.md", "/h/.claude/skills", true},
		{"prefix only", "/h/.claude/skills-extra/SKILL.md", "", false},
		{"parent", "/h/.claude", "", false},
	}
	external := []string{"/h/.codex/AGENTS.md", "/h/.claude/skills/"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := plan.ExternalFor(external, tc.path)
			if got != tc.want || found != tc.found {
				t.Errorf("ExternalFor(%s) = %q, %v", tc.path, got, found)
			}
		})
	}
}
