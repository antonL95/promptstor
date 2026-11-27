package engine_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/policy"
)

const projectPermissionID library.ID = "ps_0000000000000000000000ENG7"

func TestProjectAcksApplyToProjectItems(t *testing.T) {
	env := home(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".promptstor", "permissions", "no-remove.yaml"),
		"promptstor:\n  id: "+string(projectPermissionID)+"\ndeny_commands:\n  - rm\n")
	e := newEngine(env, adapter.Cursor)
	items, err := library.LoadLayer(filepath.Join(root, ".promptstor"), library.LayerProject, library.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(root, ".cursor", "cli.json")

	p := mustPlan(t, e, root)

	if _, found := changeFor(p, cli); found {
		t.Fatal("blocked permission planned without an acknowledgement")
	}

	acks := policy.Acks{List: []policy.Ack{
		{ItemID: projectPermissionID, Tool: adapter.Cursor, Hash: library.ContentHash(items[0])},
		{ItemID: permissionID, Tool: adapter.Cursor, Hash: library.ContentHash(itemByID(t, e, permissionID))},
	}}
	if err := acks.Save(filepath.Join(root, ".promptstor", "acks.json")); err != nil {
		t.Fatal(err)
	}
	p = mustPlan(t, e, root)

	if _, found := changeFor(p, cli); !found {
		t.Error("item acknowledged by the project is withheld")
	}
	got := slices.DeleteFunc(reportFor(p, projectPermissionID), func(u adapter.Unsupported) bool { return u.Tool != adapter.Cursor })
	if len(got) != 1 || got[0].Blocking || !strings.HasPrefix(got[0].Reason, "acknowledged: ") {
		t.Errorf("project item: %+v", got)
	}
	personal := reportFor(p, permissionID)
	if !slices.ContainsFunc(personal, func(u adapter.Unsupported) bool { return u.Tool == adapter.Cursor && u.Blocking }) {
		t.Errorf("a project acknowledgement unblocked a personal item: %+v", personal)
	}
	if len(e.Acks.List) != 0 {
		t.Errorf("project acknowledgements leaked into the user's: %+v", e.Acks.List)
	}
}
