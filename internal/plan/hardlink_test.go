//go:build unix

package plan_test

import (
	"os"
	"testing"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/testenv"
)

func TestDiffBlocksHardLinkedFile(t *testing.T) {
	env := home(t)
	write(t, at(env, "dotfiles/CLAUDE.md"), "linked rules\n")
	path := at(env, ".claude/CLAUDE.md")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Link(at(env, "dotfiles/CLAUDE.md"), path); err != nil {
		t.Fatal(err)
	}
	desired := []plan.Unit{unit(env, adapter.ClaudeCode, ".claude/CLAUDE.md", manifest.UnitSection, "", "Use spaces.\n")}

	changes := diff(t, desired, manifest.Manifest{}, []string{adapter.ClaudeCode}, plan.Options{})

	c := only(t, changes, path)
	if c.Blocked != plan.ReasonHardLinked {
		t.Fatalf("blocked %q, want %q", c.Blocked, plan.ReasonHardLinked)
	}
	before := testenv.TreeHash(t, env.Home)
	var m manifest.Manifest
	if _, err := apply(env, changes, &recorder{t: t, root: env.Home}, &m); err != nil {
		t.Fatal(err)
	}
	if testenv.TreeHash(t, env.Home) != before {
		t.Error("apply changed the hard linked file")
	}
}

func TestDiffLeavesHardLinkedFileInSyncUnblocked(t *testing.T) {
	env := home(t)
	write(t, at(env, "dotfiles/notes.md"), "a\n")
	path := at(env, "notes/a.md")
	write(t, at(env, "notes/keep.md"), "")
	if err := os.Link(at(env, "dotfiles/notes.md"), path); err != nil {
		t.Fatal(err)
	}
	desired := []plan.Unit{unit(env, adapter.Codex, "notes/a.md", manifest.UnitFile, "", "a\n")}

	changes := diff(t, desired, manifest.Manifest{}, []string{adapter.Codex}, plan.Options{})

	if c := only(t, changes, path); c.Blocked != "" || c.Op != plan.OpNoop {
		t.Errorf("op %s blocked %q", c.Op, c.Blocked)
	}
}
