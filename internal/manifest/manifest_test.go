package manifest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/library"
)

func entry(tool, path string, unit UnitKind, key string) Entry {
	return Entry{Tool: tool, Path: path, Unit: unit, Key: key, Hash: "h", Written: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func TestManifestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "manifest.json")
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != 1 || len(m.Entries) != 0 {
		t.Fatalf("missing file loaded as %+v", m)
	}
	full := entry("codex", "/b", UnitTOMLTable, "mcp_servers.x")
	full.Items = []library.ID{"rule/one", "rule/two"}
	full.Secret = true
	m.Put(full)
	m.Put(entry("claude", "/a", UnitJSON, "/z"))
	m.Put(entry("claude", "/a", UnitJSON, "/b"))
	m.Put(entry("claude", "/a", UnitFile, ""))
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Manifest{Version: 1, Entries: []Entry{
		entry("claude", "/a", UnitFile, ""),
		entry("claude", "/a", UnitJSON, "/b"),
		entry("claude", "/a", UnitJSON, "/z"),
		full,
	}}
	if !reflect.DeepEqual(loaded, want) {
		t.Errorf("got %+v\nwant %+v", loaded, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "{\n  \"version\": 1,\n  \"entries\": [\n    {\n") || !strings.HasSuffix(string(data), "}\n") {
		t.Errorf("layout:\n%s", data)
	}
}

func TestZeroManifestSavesLoadableVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	var m Manifest
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"version\": 1,\n  \"entries\": []\n}\n"; string(data) != want {
		t.Errorf("got %q", data)
	}
}

func TestLoadRejectsOtherVersionsAndBrokenFiles(t *testing.T) {
	for name, content := range map[string]string{
		"newer":   `{"version": 2, "entries": []}`,
		"missing": `{"entries": []}`,
		"broken":  `{"version": 1,`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "manifest.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), path) {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestSaveRefusesSymlinkedManifest(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "manifest.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := m.Save(link); !errors.Is(err, fsx.ErrSymlink) {
		t.Errorf("got %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("target was written: %v", err)
	}
}

func TestPutReplacesAndRemoveDeletesOneUnit(t *testing.T) {
	var m Manifest
	m.Put(entry("claude", "/a", UnitJSON, "/x"))
	m.Put(entry("claude", "/a", UnitJSON, "/y"))
	m.Put(entry("codex", "/b", UnitFile, ""))
	replaced := entry("claude", "/a", UnitJSON, "/x")
	replaced.Hash = "new"
	m.Put(replaced)

	if got, ok := m.Get("/a", UnitJSON, "/x"); !ok || got.Hash != "new" {
		t.Errorf("Get: %+v %v", got, ok)
	}
	if got := m.ForPath("/a"); len(got) != 2 {
		t.Errorf("ForPath: %+v", got)
	}
	if got := m.ForTool("codex"); len(got) != 1 || got[0].Path != "/b" {
		t.Errorf("ForTool: %+v", got)
	}
	m.Remove("/a", UnitJSON, "/x")
	if _, ok := m.Get("/a", UnitJSON, "/x"); ok {
		t.Error("entry still present")
	}
	if len(m.Entries) != 2 {
		t.Errorf("entries: %+v", m.Entries)
	}
}

func mustHash(t *testing.T, unit UnitKind, content string) string {
	t.Helper()
	got, err := Hash(unit, []byte(content))
	if err != nil {
		t.Fatalf("Hash(%s, %q): %v", unit, content, err)
	}
	if len(got) != 64 {
		t.Fatalf("Hash(%s, %q) = %q", unit, content, got)
	}
	return got
}

type hashPair struct{ name, left, right string }

func assertHashes(t *testing.T, unit UnitKind, same bool, pairs []hashPair) {
	t.Helper()
	for _, pair := range pairs {
		t.Run(pair.name, func(t *testing.T) {
			if got := mustHash(t, unit, pair.left) == mustHash(t, unit, pair.right); got != same {
				t.Errorf("equal hashes = %v for %q and %q", got, pair.left, pair.right)
			}
		})
	}
}

func TestHashJSONIgnoresKeyOrderAndWhitespace(t *testing.T) {
	for _, unit := range []UnitKind{UnitJSON, UnitJSONElement} {
		assertHashes(t, unit, true, []hashPair{
			{"order and space", `{"a":1,"b":{"c":[1,2],"d":"x"}}`, "{\n  \"b\": {\"d\": \"x\", \"c\": [1, 2]},\n  \"a\": 1\n}\n"},
			{"comments and commas", `{"a":[1]}`, "{\n // note\n \"a\": [1,], /* note */\n}"},
			{"scalar", `"x"`, ` "x" `},
		})
		assertHashes(t, unit, false, []hashPair{
			{"value", `{"a":1}`, `{"a":2}`},
			{"array order", `[1,2]`, `[2,1]`},
			{"big numbers", `9007199254740993`, `9007199254740992`},
			{"type", `{"a":1}`, `{"a":"1"}`},
		})
	}
}

func TestHashJSONLeavesInputUntouched(t *testing.T) {
	original := "{\n // note\n \"a\": 1,\n}"
	content := []byte(original)
	if _, err := Hash(UnitJSON, content); err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Errorf("input changed to %q", content)
	}
}

func TestHashTOMLIgnoresFormatting(t *testing.T) {
	assertHashes(t, UnitTOMLTable, true, []hashPair{
		{"order, space, comments, quotes", "[mcp.x]\ncommand = \"run\"\nargs = [\"a\", \"b\"]\n", "# note\n[mcp.x]\nargs=[\n  'a',\n  'b',\n]\n\ncommand   =   'run' # note\n"},
	})
	assertHashes(t, UnitTOMLTable, false, []hashPair{
		{"value", "[a]\nb = 1\n", "[a]\nb = 2\n"},
		{"table name", "[a]\nb = 1\n", "[c]\nb = 1\n"},
	})
}

func TestHashTextIgnoresLineEndingsAndTrailingSpace(t *testing.T) {
	for _, unit := range []UnitKind{UnitFile, UnitSection} {
		assertHashes(t, unit, true, []hashPair{
			{"crlf and trailing blanks", "a\n  b\n", "a  \r\n  b\t\r\n"},
		})
		assertHashes(t, unit, false, []hashPair{
			{"leading space", "a\n", " a\n"},
			{"blank line", "a\nb\n", "a\n\nb\n"},
		})
	}
}

func TestHashParseFailureIsAnErrorWithoutContent(t *testing.T) {
	const secret = "sk-synthetic-secret"
	for name, test := range map[string]struct {
		unit    UnitKind
		content string
	}{
		"json bare word":    {UnitJSON, `{"token": ` + secret + `}`},
		"json empty":        {UnitJSON, ""},
		"element trailing":  {UnitJSONElement, `{} ` + secret},
		"toml bare word":    {UnitTOMLTable, "[a]\ntoken = " + secret + "\n"},
		"toml duplicate":    {UnitTOMLTable, secret + " = 1\n" + secret + " = 2\n"},
		"unknown unit kind": {UnitKind("yaml"), "a"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Hash(test.unit, []byte(test.content))
			if err == nil || got != "" {
				t.Fatalf("got %q %v", got, err)
			}
			if strings.Contains(err.Error(), "sk-") {
				t.Error("error repeats the content")
			}
		})
	}
}

func TestElementKeyParseAllThreeForms(t *testing.T) {
	hash := strings.Repeat("0a", 32)
	tests := []struct {
		key  string
		want Key
	}{
		{"/hooks/Stop@/command=run", Key{Pointer: "/hooks/Stop", Sub: "/command", Value: "run", Form: KeyFormMatch}},
		{"/a@/b/c=x=y@z", Key{Pointer: "/a", Sub: "/b/c", Value: "x=y@z", Form: KeyFormMatch}},
		{"/a@/b=", Key{Pointer: "/a", Sub: "/b", Form: KeyFormMatch}},
		{"/permissions/deny@=Bash(rm@=*)", Key{Pointer: "/permissions/deny", Value: "Bash(rm@=*)", Form: KeyFormScalar}},
		{"@=x", Key{Value: "x", Form: KeyFormScalar}},
		{"/permissions@#" + hash, Key{Pointer: "/permissions", Hash: hash, Form: KeyFormHash}},
		{"/a~1b/~0@=x", Key{Pointer: "/a~1b/~0", Value: "x", Form: KeyFormScalar}},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			got, err := ParseKey(UnitJSONElement, test.key)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Errorf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestParseKeyForOtherUnits(t *testing.T) {
	tests := []struct {
		unit UnitKind
		key  string
		want Key
	}{
		{UnitJSON, "/mcpServers/x", Key{Pointer: "/mcpServers/x"}},
		{UnitJSON, "", Key{}},
		{UnitTOMLTable, "mcp_servers.x", Key{Pointer: "mcp_servers.x"}},
		{UnitFile, "", Key{}},
		{UnitSection, "", Key{}},
	}
	for _, test := range tests {
		got, err := ParseKey(test.unit, test.key)
		if err != nil || got != test.want {
			t.Errorf("ParseKey(%s, %q) = %+v, %v", test.unit, test.key, got, err)
		}
	}
}

func TestParseKeyRejectsMalformedKeys(t *testing.T) {
	tests := []struct {
		unit UnitKind
		key  string
	}{
		{UnitJSONElement, ""},
		{UnitJSONElement, "/a"},
		{UnitJSONElement, "/a@"},
		{UnitJSONElement, "a@=x"},
		{UnitJSONElement, "/a@b=x"},
		{UnitJSONElement, "/a@/b"},
		{UnitJSONElement, "/a@#abc"},
		{UnitJSONElement, "/a@#" + strings.Repeat("g", 64)},
		{UnitJSONElement, "/a@#" + strings.Repeat("A", 64)},
		{UnitJSONElement, "/a~2@=x"},
		{UnitJSONElement, "/a@/b~=x"},
		{UnitJSON, "a/b"},
		{UnitJSON, "/a~"},
		{UnitTOMLTable, ""},
		{UnitFile, "x"},
		{UnitSection, "x"},
		{UnitKind("yaml"), "x"},
	}
	for _, test := range tests {
		_, err := ParseKey(test.unit, test.key)
		if err == nil || !strings.Contains(err.Error(), strconv.Quote(test.key)) {
			t.Errorf("ParseKey(%s, %q): %v", test.unit, test.key, err)
		}
	}
}

func TestElementKeyDoesNotSupportAtInArrayPointer(t *testing.T) {
	if _, err := ParseKey(UnitJSONElement, "/a@b@=x"); err == nil {
		t.Error("expected an error")
	}
}
