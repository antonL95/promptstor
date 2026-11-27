package plan_test

import (
	"testing"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

func TestDiffMarksDeletionOfFileEditedByHand(t *testing.T) {
	for name, current := range map[string]string{"edited": "a, edited by hand\n", "unchanged": "a\n"} {
		t.Run(name, func(t *testing.T) {
			env := home(t)
			path := at(env, "notes/a.md")
			write(t, path, current)
			hash, err := manifest.Hash(manifest.UnitFile, []byte("a\n"))
			if err != nil {
				t.Fatal(err)
			}
			var m manifest.Manifest
			m.Put(manifest.Entry{Tool: adapter.Codex, Path: path, Root: env.Home, Unit: manifest.UnitFile, Hash: hash})

			changes := diff(t, nil, m, []string{adapter.Codex}, plan.Options{})

			c := only(t, changes, path)
			if c.Op != plan.OpDelete {
				t.Fatalf("op %s blocked %q", c.Op, c.Blocked)
			}
			if want := name == "edited"; c.EditedByHand != want {
				t.Errorf("edited by hand %v, want %v", c.EditedByHand, want)
			}
		})
	}
}
