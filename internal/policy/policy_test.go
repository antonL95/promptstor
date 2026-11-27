package policy

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
)

const tool = "example-tool"

func permission(id library.ID, portable library.PortablePermission) library.Item {
	return library.Item{ID: id, Kind: library.KindPermission, Name: string(id), Portable: &portable}
}

func denyBoth() library.Item {
	return permission("ps_deny", library.PortablePermission{
		DenyCommands:    []string{"rm -rf"},
		DenyPaths:       []string{"**/*.example-secret"},
		AllowMCPServers: []string{"example-docs"},
	})
}

func acknowledge(item library.Item) Acks {
	var acks Acks
	acks.Add(Ack{ItemID: item.ID, Tool: tool, Hash: library.ContentHash(item)})
	return acks
}

func unsupported(item library.Item, reason string, blocking bool) adapter.Unsupported {
	return adapter.Unsupported{ItemID: item.ID, Name: item.Name, Tool: tool, Reason: reason, Blocking: blocking}
}

func TestGateBlocksInexpressibleDeny(t *testing.T) {
	item := denyBoth()
	rule := library.Item{ID: "ps_rule", Kind: library.KindRule, Name: "rule"}
	passed, report := Gate([]library.Item{item, rule}, tool, adapter.PortableCaps{}, Acks{})
	if want := []library.Item{rule}; !reflect.DeepEqual(passed, want) {
		t.Errorf("passed = %+v", passed)
	}
	want := []adapter.Unsupported{
		unsupported(item, "deny commands cannot be expressed", true),
		unsupported(item, "deny paths cannot be expressed", true),
		unsupported(item, "allowed MCP servers cannot be expressed", false),
	}
	if !reflect.DeepEqual(report, want) {
		t.Errorf("report = %+v", report)
	}
}

func TestGateBlocksWhenOneDenyCategoryIsInexpressible(t *testing.T) {
	caps := adapter.PortableCaps{DenyCommands: true, AllowMCPServers: true}
	item := denyBoth()
	passed, report := Gate([]library.Item{item}, tool, caps, Acks{})
	if len(passed) != 0 {
		t.Errorf("passed = %+v", passed)
	}
	want := []adapter.Unsupported{unsupported(item, "deny paths cannot be expressed", true)}
	if !reflect.DeepEqual(report, want) {
		t.Errorf("report = %+v", report)
	}
}

func TestGatePassesAfterAckButStillReports(t *testing.T) {
	item := denyBoth()
	input := []library.Item{item}
	passed, report := Gate(input, tool, adapter.PortableCaps{DenyCommands: true}, acknowledge(item))
	if len(passed) != 1 {
		t.Fatalf("passed = %+v", passed)
	}
	want := library.PortablePermission{DenyCommands: []string{"rm -rf"}}
	if !reflect.DeepEqual(*passed[0].Portable, want) {
		t.Errorf("portable = %+v", *passed[0].Portable)
	}
	if !reflect.DeepEqual(input, []library.Item{denyBoth()}) {
		t.Errorf("input was mutated: %+v", *input[0].Portable)
	}
	wantReport := []adapter.Unsupported{
		unsupported(item, "acknowledged: deny paths cannot be expressed", false),
		unsupported(item, "allowed MCP servers cannot be expressed", false),
	}
	if !reflect.DeepEqual(report, wantReport) {
		t.Errorf("report = %+v", report)
	}
}

func TestAckInvalidatedWhenItemChanges(t *testing.T) {
	item := denyBoth()
	acks := acknowledge(item)
	changed := denyBoth()
	changed.Portable.DenyPaths = append(changed.Portable.DenyPaths, "**/other")
	passed, report := Gate([]library.Item{changed}, tool, adapter.PortableCaps{}, acks)
	if len(passed) != 0 || len(report) != 3 || !report[0].Blocking {
		t.Errorf("passed = %+v, report = %+v", passed, report)
	}
}

func TestGateStripsAllowedServersWithoutBlocking(t *testing.T) {
	item := denyBoth()
	caps := adapter.PortableCaps{DenyCommands: true, DenyPaths: true}
	passed, report := Gate([]library.Item{item}, tool, caps, Acks{})
	if len(passed) != 1 || passed[0].Portable.AllowMCPServers != nil || len(passed[0].Portable.DenyPaths) != 1 {
		t.Fatalf("passed = %+v", passed)
	}
	if len(item.Portable.AllowMCPServers) != 1 {
		t.Error("input was mutated")
	}
	want := []adapter.Unsupported{unsupported(item, "allowed MCP servers cannot be expressed", false)}
	if !reflect.DeepEqual(report, want) {
		t.Errorf("report = %+v", report)
	}
}

func TestGateLeavesOtherItemsUnchangedAndInOrder(t *testing.T) {
	items := []library.Item{
		{ID: "ps_native", Kind: library.KindPermission, Name: "native", Native: map[string]any{"claude": true}},
		{ID: "ps_rule", Kind: library.KindRule, Name: "rule"},
		permission("ps_empty", library.PortablePermission{}),
	}
	passed, report := Gate(items, tool, adapter.PortableCaps{}, Acks{})
	if !reflect.DeepEqual(passed, items) || report != nil {
		t.Errorf("passed = %+v, report = %+v", passed, report)
	}
}

func TestAckMatchesItemToolAndHash(t *testing.T) {
	acks := Acks{List: []Ack{{ItemID: "ps_a", Tool: "one", Hash: "h"}, {ItemID: "ps_b", Tool: "one"}}}
	for name, tc := range map[string]struct {
		id         library.ID
		tool, hash string
		want       bool
	}{
		"match":      {"ps_a", "one", "h", true},
		"other item": {"ps_c", "one", "h", false},
		"other tool": {"ps_a", "two", "h", false},
		"other hash": {"ps_a", "one", "i", false},
		"empty hash": {"ps_b", "one", "", false},
	} {
		if got := acks.Has(tc.id, tc.tool, tc.hash); got != tc.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}

func TestAcksSaveSortedAndLoadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "acks.json")
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var acks Acks
	acks.Add(Ack{ItemID: "ps_b", Tool: "one", Hash: "h1", At: at})
	acks.Add(Ack{ItemID: "ps_a", Tool: "two", Hash: "h2", At: at})
	acks.Add(Ack{ItemID: "ps_a", Tool: "one", Hash: "old", At: at})
	acks.Add(Ack{ItemID: "ps_a", Tool: "one", Hash: "h3", At: at})
	if err := acks.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "acks": [
    {
      "item_id": "ps_a",
      "tool": "one",
      "hash": "h3",
      "at": "2026-01-02T03:04:05Z"
    },
    {
      "item_id": "ps_a",
      "tool": "two",
      "hash": "h2",
      "at": "2026-01-02T03:04:05Z"
    },
    {
      "item_id": "ps_b",
      "tool": "one",
      "hash": "h1",
      "at": "2026-01-02T03:04:05Z"
    }
  ]
}
`
	if string(data) != want {
		t.Errorf("saved:\n%s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fs.FileMode(0o600) {
		t.Errorf("mode = %o", info.Mode().Perm())
	}
	loaded, err := LoadAcks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.List) != 3 || !loaded.Has("ps_a", "one", "h3") || loaded.Has("ps_a", "one", "old") {
		t.Errorf("loaded = %+v", loaded)
	}
}

func TestLoadAcksMissingAndBroken(t *testing.T) {
	dir := t.TempDir()
	acks, err := LoadAcks(filepath.Join(dir, "acks.json"))
	if err != nil || len(acks.List) != 0 {
		t.Errorf("missing: %+v, %v", acks, err)
	}
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAcks(broken); err == nil {
		t.Error("broken file loaded")
	}
}
