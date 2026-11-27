package preview

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

func TestPlanWarnsAboutDeletingFileEditedByHand(t *testing.T) {
	changes := []plan.FileChange{
		{Path: "/x/edited.md", Op: plan.OpDelete, Before: []byte("edited\n"), EditedByHand: true},
		{Path: "/x/clean.md", Op: plan.OpDelete, Before: []byte("clean\n")},
	}
	var out bytes.Buffer

	if _, err := Plan(&out, engine.Plan{Changes: changes}, false); err != nil {
		t.Fatal(err)
	}

	want := "deleting a file edited by hand: /x/edited.md\n" +
		"2 files to change, 0 blocked, 0 unsupported\n"
	if out.String() != want {
		t.Errorf("got\n%s\nwant\n%s", out.String(), want)
	}
}

func TestBlockedListGroupsBySymlink(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "dotfiles", "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("dotfiles", "claude"), filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	unmanaged := filepath.Join(home, "notes", "rules.md")
	if err := os.MkdirAll(filepath.Dir(unmanaged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unmanaged, []byte("hand written\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := func(path string) plan.Unit {
		return plan.Unit{Tool: "claude-code", Path: path, Root: home, Kind: manifest.UnitFile, Content: []byte("x\n"), Mode: 0o644}
	}
	desired := []plan.Unit{
		unit(filepath.Join(home, ".claude", "agents", "a.md")),
		unit(filepath.Join(home, ".claude", "skills", "b", "SKILL.md")),
		unit(filepath.Join(home, ".claude", "commands", "c.md")),
		unit(unmanaged),
	}
	changes, err := plan.Diff(desired, manifest.Manifest{}, []string{"claude-code"}, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer

	blocked, err := Plan(&out, engine.Plan{Changes: changes}, false)
	if err != nil {
		t.Fatal(err)
	}

	want := "blocked:\n" +
		"  " + filepath.Join(home, ".claude") + " (symlink to " + filepath.Join("dotfiles", "claude") + "): 3 files\n" +
		"  " + unmanaged + ": not created by promptstor\n" +
		"0 files to change, 4 blocked, 0 unsupported\n"
	if out.String() != want {
		t.Errorf("got\n%s\nwant\n%s", out.String(), want)
	}
	if blocked != 4 {
		t.Errorf("blocked count %d", blocked)
	}
}
