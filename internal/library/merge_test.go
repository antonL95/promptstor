package library

import (
	"reflect"
	"slices"
	"testing"
)

func rule(id ID, name string) Item {
	return Item{ID: id, Kind: KindRule, Name: name, Body: name + "\n"}
}

func locked(it Item) Item {
	it.Locked = true
	return it
}

func tombstone(id ID, name string) Item {
	return Item{ID: id, Kind: KindRule, Name: name, Removed: true}
}

func layer(name Layer, items ...Item) LayerItems {
	return LayerItems{Layer: name, Items: items}
}

func merged(t *testing.T, layers ...LayerItems) Merged {
	t.Helper()
	result, violations := Merge(layers)
	if len(violations) != 0 {
		t.Fatalf("unexpected violations: %+v", violations)
	}
	return result
}

func names(items []Item) []string {
	found := []string{}
	for _, item := range items {
		found = append(found, item.Name)
	}
	return found
}

func expectNames(t *testing.T, items []Item, want ...string) {
	t.Helper()
	if got := names(items); !slices.Equal(got, append([]string{}, want...)) {
		t.Fatalf("items = %q, want %q", got, want)
	}
}

func TestLaterLayerOverridesByID(t *testing.T) {
	ranked := []Layer{LayerCompany, LayerTeam, LayerProject, LayerPersonal, LayerDevice}
	for index := 1; index < len(ranked); index++ {
		earlier, later := ranked[index-1], ranked[index]
		t.Run(string(later)+" over "+string(earlier), func(t *testing.T) {
			result := merged(t,
				layer(later, rule("ps_a", "new")),
				layer(earlier, rule("ps_a", "old")),
			)
			expectNames(t, result.Items, "new")
			if result.Items[0].Layer != later {
				t.Fatalf("layer = %q, want %q", result.Items[0].Layer, later)
			}
		})
	}
}

func TestRemovedTombstoneDeletes(t *testing.T) {
	result := merged(t,
		layer(LayerCompany, rule("ps_a", "gone"), rule("ps_b", "kept")),
		layer(LayerPersonal, tombstone("ps_a", "gone")),
	)
	expectNames(t, result.Items, "kept")
}

func TestTombstoneWithoutEarlierMatchIsDropped(t *testing.T) {
	result := merged(t,
		layer(LayerCompany, tombstone("ps_a", "never-there")),
		layer(LayerPersonal, rule("ps_b", "kept"), tombstone("ps_c", "unknown")),
	)
	expectNames(t, result.Items, "kept")
}

func TestLaterLayerAddsRemovedItemAgain(t *testing.T) {
	result := merged(t,
		layer(LayerCompany, rule("ps_a", "first")),
		layer(LayerTeam, tombstone("ps_a", "first")),
		layer(LayerPersonal, rule("ps_a", "again")),
	)
	expectNames(t, result.Items, "again")
}

func TestLockedTeamItemKeptAndViolationReported(t *testing.T) {
	cases := []struct {
		name      string
		locking   Layer
		offending Layer
		attempt   Item
	}{
		{"override of team item", LayerTeam, LayerPersonal, rule("ps_a", "other")},
		{"tombstone of team item", LayerTeam, LayerDevice, tombstone("ps_a", "other")},
		{"override of company item", LayerCompany, LayerTeam, locked(rule("ps_a", "other"))},
		{"tombstone of company item", LayerCompany, LayerProject, tombstone("ps_a", "other")},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result, violations := Merge([]LayerItems{
				layer(test.offending, test.attempt),
				layer(test.locking, locked(rule("ps_a", "kept"))),
			})
			expectNames(t, result.Items, "kept")
			kept := result.Items[0]
			if kept.Body != "kept\n" || kept.Layer != test.locking {
				t.Fatalf("locked item changed: %+v", kept)
			}
			want := []Violation{{
				ItemID: "ps_a",
				Name:   "kept",
				Layer:  test.offending,
				Reason: "locked by " + string(test.locking),
			}}
			if !reflect.DeepEqual(violations, want) {
				t.Fatalf("violations = %+v, want %+v", violations, want)
			}
		})
	}
}

func TestViolationsPerOffendingItemInStableOrder(t *testing.T) {
	company := layer(LayerCompany, locked(rule("ps_a", "alpha")), locked(rule("ps_b", "beta")))
	device := layer(LayerDevice, tombstone("ps_a", "alpha"))
	personal := []Item{rule("ps_b", "mine"), rule("ps_a", "mine too")}
	want := []Violation{
		{ItemID: "ps_a", Name: "alpha", Layer: LayerPersonal, Reason: "locked by company"},
		{ItemID: "ps_b", Name: "beta", Layer: LayerPersonal, Reason: "locked by company"},
		{ItemID: "ps_a", Name: "alpha", Layer: LayerDevice, Reason: "locked by company"},
	}
	for range 2 {
		_, violations := Merge([]LayerItems{device, company, layer(LayerPersonal, personal...)})
		if !reflect.DeepEqual(violations, want) {
			t.Fatalf("violations = %+v, want %+v", violations, want)
		}
		slices.Reverse(personal)
	}
}

func TestLockOnPersonalHasNoEffect(t *testing.T) {
	for _, unlockable := range []Layer{LayerProject, LayerPersonal} {
		t.Run(string(unlockable), func(t *testing.T) {
			result := merged(t,
				layer(unlockable, locked(rule("ps_a", "old")), locked(rule("ps_b", "removed"))),
				layer(LayerDevice, rule("ps_a", "new"), tombstone("ps_b", "removed")),
			)
			expectNames(t, result.Items, "new")
		})
	}
}

func TestUnknownLayerSortsAfterDevice(t *testing.T) {
	result := merged(t,
		layer("zeta", rule("ps_a", "zeta")),
		layer("alpha", rule("ps_a", "alpha"), rule("ps_b", "alpha only")),
		layer(LayerDevice, rule("ps_a", "device"), locked(rule("ps_b", "device only"))),
	)
	expectNames(t, result.Items, "alpha only", "zeta")
}

func TestForToolIncludeExclude(t *testing.T) {
	targeted := func(id ID, name string, include, exclude []string) Item {
		item := rule(id, name)
		item.Targets = Targets{Include: include, Exclude: exclude}
		return item
	}
	result := merged(t, layer(LayerPersonal,
		targeted("ps_a", "everywhere", nil, nil),
		targeted("ps_b", "only codex", []string{"codex"}, nil),
		targeted("ps_c", "not codex", nil, []string{"codex"}),
		targeted("ps_d", "exclude wins", []string{"codex", "claude"}, []string{"codex"}),
	))
	before := slices.Clone(result.Items)

	expectNames(t, result.ForTool("codex"), "everywhere", "only codex")
	claude := result.ForTool("claude")
	expectNames(t, claude, "everywhere", "exclude wins", "not codex")

	claude[0].Name = "changed"
	if !reflect.DeepEqual(result.Items, before) {
		t.Fatalf("ForTool mutated the merged items: %q", names(result.Items))
	}
}

func TestSortOrderStable(t *testing.T) {
	item := func(id ID, kind Kind, order int, name string) Item {
		return Item{ID: id, Kind: kind, Order: order, Name: name}
	}
	items := []Item{
		item("ps_01", KindRule, -1, "z first by order"),
		item("ps_02", KindRule, 0, "a"),
		item("ps_03", KindRule, 0, "b"),
		item("ps_04", KindRule, 0, "b"),
		item("ps_05", KindRule, 5, "a last by order"),
		item("ps_06", KindSkill, -9, "skill"),
		item("ps_07", KindCommand, 0, "command"),
		item("ps_08", KindSubagent, 0, "subagent"),
		item("ps_09", KindPermission, 0, "permission"),
		item("ps_10", KindHook, 0, "hook"),
		item("ps_11", KindMCP, 0, "mcp"),
	}
	want := []ID{"ps_01", "ps_02", "ps_03", "ps_04", "ps_05", "ps_06", "ps_07", "ps_08", "ps_09", "ps_10", "ps_11"}
	for range 2 {
		var got []ID
		for _, sorted := range merged(t, layer(LayerPersonal, items...)).Items {
			got = append(got, sorted.ID)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("order = %q, want %q", got, want)
		}
		slices.Reverse(items)
	}
}

func TestMergedHashChangesWithContent(t *testing.T) {
	first, second := rule("ps_a", "first"), rule("ps_b", "second")
	base := merged(t, layer(LayerCompany, first), layer(LayerPersonal, second)).Hash()
	if len(base) != 64 {
		t.Fatalf("hash = %q, want 64 hex characters", base)
	}

	reordered := merged(t, layer(LayerPersonal, second), layer(LayerCompany, first)).Hash()
	if reordered != base {
		t.Fatal("input order changed the hash")
	}
	if together := merged(t, layer(LayerPersonal, second, first)).Hash(); together != base {
		t.Fatal("equal content gave a different hash")
	}

	edited := second
	edited.Body = "edited\n"
	renumbered := second
	renumbered.ID = "ps_c"
	changes := map[string]Merged{
		"edited body":  merged(t, layer(LayerPersonal, first, edited)),
		"changed id":   merged(t, layer(LayerPersonal, first, renumbered)),
		"removed item": merged(t, layer(LayerPersonal, first)),
		"empty":        merged(t),
	}
	for name, changed := range changes {
		if changed.Hash() == base {
			t.Errorf("%s kept the hash", name)
		}
	}
}
