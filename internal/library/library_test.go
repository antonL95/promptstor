package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const fixture = "testdata/layer"

func load(t *testing.T, dir string) []Item {
	t.Helper()
	items, err := LoadLayer(dir, LayerPersonal, LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func find(t *testing.T, items []Item, kind Kind, name string) Item {
	t.Helper()
	for _, item := range items {
		if item.Kind == kind && item.Name == name {
			return item
		}
	}
	t.Fatalf("no %s named %q", kind, name)
	return Item{}
}

func put(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func write(t *testing.T, dir string, item Item) string {
	t.Helper()
	path, err := WriteItem(dir, item)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = read(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func assertError(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want one mentioning %q", parts)
	}
	for _, part := range parts {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not mention %q", err, part)
		}
	}
}

func document(id, rest string) string {
	return "---\npromptstor:\n  id: " + id + "\n" + rest + "---\nbody\n"
}

func TestRoundTripEveryKind(t *testing.T) {
	items := load(t, fixture)
	out := t.TempDir()
	kinds := map[Kind]bool{}

	for _, item := range items {
		kinds[item.Kind] = true
		write(t, out, item)
	}

	if len(kinds) != 7 {
		t.Errorf("fixture covers %d kinds, want 7", len(kinds))
	}
	want, got := tree(t, fixture), tree(t, out)
	if len(got) != len(want) {
		t.Errorf("wrote %d files, want %d", len(got), len(want))
	}
	for name, content := range want {
		if got[name] != content {
			t.Errorf("%s differs\n--- want\n%s\n--- got\n%s", name, content, got[name])
		}
	}
}

func TestLoadReadsEveryField(t *testing.T) {
	items := load(t, fixture)
	source, err := filepath.Abs(filepath.Join(fixture, "rules", "code-comments.md"))
	if err != nil {
		t.Fatal(err)
	}

	rule := find(t, items, KindRule, "Code comments")
	want := Targets{Include: []string{"claude", "codex"}, Exclude: []string{"cursor"}}
	if rule.ID != "ps_00000000000000000000000001" || rule.Order != 10 || !rule.Locked || rule.Removed {
		t.Errorf("rule = %+v", rule)
	}
	if !reflect.DeepEqual(rule.Targets, want) || !slices.Equal(rule.Scope, []string{"**/*.go"}) {
		t.Errorf("targets = %+v, scope = %v", rule.Targets, rule.Scope)
	}
	if rule.Activation != ActivationFiles || rule.Layer != LayerPersonal || rule.Source != source {
		t.Errorf("activation = %q, layer = %q, source = %q", rule.Activation, rule.Layer, rule.Source)
	}
	if rule.Body != "\n# Code comments\n\nWrite no comments.\n" {
		t.Errorf("body = %q", rule.Body)
	}

	short := find(t, items, KindRule, "short")
	if short.Body != "Short rule without a trailing newline" || !short.Removed || short.Activation != "" {
		t.Errorf("short = %+v", short)
	}

	permission := find(t, items, KindPermission, "no-force-push")
	portable := &PortablePermission{
		DenyCommands:    []string{"git push --force"},
		DenyPaths:       []string{"**/*.example-secret"},
		AllowMCPServers: []string{"example-docs"},
	}
	if !reflect.DeepEqual(permission.Portable, portable) || permission.Native["claude"] == nil {
		t.Errorf("portable = %+v, native = %v", permission.Portable, permission.Native)
	}

	hook := find(t, items, KindHook, "Format check")
	if string(hook.Files["check.sh"].Data) != "#!/bin/sh\nexit 0\n" || hook.Native["claude"] == nil {
		t.Errorf("hook = %+v", hook)
	}

	server := find(t, items, KindMCP, "example-docs")
	mcp := &MCPServer{
		Transport: "stdio",
		Command:   "example-server",
		Args:      []string{"--port", "8080"},
		Env: map[string]Value{
			"EXAMPLE_MODE":  {Literal: "readonly"},
			"EXAMPLE_TOKEN": {Ref: "env:EXAMPLE_TOKEN"},
		},
		Resolve: true,
	}
	if !reflect.DeepEqual(server.MCP, mcp) {
		t.Errorf("mcp = %+v", server.MCP)
	}
	remote := find(t, items, KindMCP, "example-remote")
	if remote.MCP.URL != "https://mcp.example.com/v1" || remote.MCP.Headers["Authorization"].Ref != "env:EXAMPLE_AUTH" {
		t.Errorf("remote = %+v", remote.MCP)
	}
}

func TestUnknownFrontmatterPreserved(t *testing.T) {
	skill := find(t, load(t, fixture), KindSkill, "release-notes")
	if skill.Meta["model"] != "example-model" {
		t.Errorf("model = %v", skill.Meta["model"])
	}
	if !reflect.DeepEqual(skill.Meta["allowed-tools"], []any{"Read", "Bash"}) {
		t.Errorf("allowed-tools = %v", skill.Meta["allowed-tools"])
	}
	skill.Meta["model"] = "other-model"
	skill.Meta["added"] = true
	delete(skill.Meta, "allowed-tools")
	out := t.TempDir()

	got := read(t, write(t, out, skill))

	want := "x-example-unknown:\n  zeta: 1\n  alpha: \"two\"\nmodel: other-model\nadded: true\n---\n"
	if !strings.Contains(got, want) || strings.Contains(got, "allowed-tools") {
		t.Errorf("wrote\n%s", got)
	}
}

func TestMetaValuesSurviveRewrite(t *testing.T) {
	dir := t.TempDir()
	source := document("ps_00000000000000000000000001", "folded: >\n  one\n  two\n\n  three\nliteral: |-\n  a\n    b\nempty:\nnested: {list: [1, '2', true], deeper: {x: y}}\ndated: 2026-01-02\n")
	put(t, dir, "rules/a.md", source)
	before := load(t, dir)[0]
	out := t.TempDir()

	write(t, out, before)

	after := load(t, out)[0]
	if !reflect.DeepEqual(after.Meta, before.Meta) {
		t.Errorf("meta changed\n--- before\n%#v\n--- after\n%#v", before.Meta, after.Meta)
	}
}

func TestLoadNonCanonicalThenWriteIsCanonical(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "rules/loose.md", "\ufeff---\r\nzebra: 1\r\npromptstor: {locked: false, order: 0, id: ps_00000000000000000000000001}\r\ndescription: \"Loose\"\r\n---\r\nbody\r\n")
	put(t, dir, "mcp/loose.yaml", "env: {B: {ref: 'env:B'}, A: 1}\ntransport: stdio\npromptstor: {id: ps_00000000000000000000000002}\n")
	out := t.TempDir()

	for _, item := range load(t, dir) {
		write(t, out, item)
	}

	want := map[string]string{
		"rules/loose.md": "---\ndescription: Loose\npromptstor:\n  id: ps_00000000000000000000000001\nzebra: 1\n---\nbody\r\n",
		"mcp/loose.yaml": "promptstor:\n  id: ps_00000000000000000000000002\ntransport: stdio\nenv:\n  A: \"1\"\n  B:\n    ref: env:B\n",
	}
	if got := tree(t, out); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

func TestDuplicateIDRejected(t *testing.T) {
	dir := t.TempDir()
	first := put(t, dir, "rules/first.md", document("ps_00000000000000000000000001", ""))
	second := put(t, dir, "commands/second.md", document("ps_00000000000000000000000001", ""))

	_, err := LoadLayer(dir, LayerPersonal, LoadOptions{})

	assertError(t, err, first, second)
}

func TestMissingIDErrorsWithoutAssign(t *testing.T) {
	dir := t.TempDir()
	path := put(t, dir, "rules/new.md", "body\n")

	_, err := LoadLayer(dir, LayerPersonal, LoadOptions{})

	assertError(t, err, path, "id")
	if got := read(t, path); got != "body\n" {
		t.Errorf("file changed to %q", got)
	}
}

func TestAssignIDsWritesIDBack(t *testing.T) {
	dir := t.TempDir()
	rule := put(t, dir, "rules/new.md", "body\n")
	hook := put(t, dir, "hooks/new.yaml", "native:\n  claude:\n    event: Stop\n")

	items, err := LoadLayer(dir, LayerPersonal, LoadOptions{AssignIDs: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 2 || items[0].ID == "" || items[1].ID == "" {
		t.Fatalf("items = %+v", items)
	}
	first := find(t, items, KindRule, "new")
	if got, want := read(t, rule), "---\npromptstor:\n  id: "+string(first.ID)+"\n---\nbody\n"; got != want {
		t.Errorf("rule = %q, want %q", got, want)
	}
	if !strings.HasPrefix(read(t, hook), "promptstor:\n  id: ps_") {
		t.Errorf("hook = %q", read(t, hook))
	}
	if again := find(t, load(t, dir), KindRule, "new"); again.ID != first.ID {
		t.Errorf("id changed from %s to %s", first.ID, again.ID)
	}
}

func TestAssignIDsWritesNothingWhenTheLayerIsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := put(t, dir, "rules/a.md", "body\n")
	put(t, dir, "rules/b.md", "---\nunclosed\n")

	if _, err := LoadLayer(dir, LayerPersonal, LoadOptions{AssignIDs: true}); err == nil {
		t.Fatal("no error")
	}
	if got := read(t, path); got != "body\n" {
		t.Errorf("file changed to %q", got)
	}
}

func TestIDSurvivesRename(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "rules/renamed.md", document("ps_00000000000000000000000001", ""))

	item := load(t, dir)[0]

	if item.ID != "ps_00000000000000000000000001" || item.Name != "renamed" {
		t.Errorf("item = %+v", item)
	}
}

func TestNewIDShape(t *testing.T) {
	shape := regexp.MustCompile(`^ps_[0-9A-HJKMNP-TV-Z]{26}$`)
	first, second := NewID(), NewID()
	if !shape.MatchString(string(first)) || first == second {
		t.Errorf("ids = %q, %q", first, second)
	}
}

func TestSkillAttachmentsKeepMode(t *testing.T) {
	skill := find(t, load(t, fixture), KindSkill, "release-notes")
	if got := skill.Files["scripts/run.sh"].Mode; got != 0o755 {
		t.Fatalf("loaded mode = %o", got)
	}
	if _, ok := skill.Files["SKILL.md"]; ok || len(skill.Files) != 2 {
		t.Errorf("files = %v", skill.Files)
	}
	out := t.TempDir()
	script := put(t, out, "skills/release-notes/scripts/run.sh", "old")

	write(t, out, skill)

	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("written mode = %o", info.Mode().Perm())
	}
}

func TestSlug(t *testing.T) {
	long := strings.Repeat("a", 63)
	for name, want := range map[string]string{
		"Code comments":   "code-comments",
		"  --Git & PRs!!": "git-prs",
		"Čeština":         "e-tina",
		"!!!":             "",
		long + " bcd":     long,
		long + "bcd":      long + "b",
	} {
		if got := Slug(name); got != want {
			t.Errorf("Slug(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestInvalidSkillSlugRejected(t *testing.T) {
	for _, name := range []string{
		"skills/Bad_Name/SKILL.md",
		"skills/-lead/SKILL.md",
		"commands/Bad Name.md",
	} {
		dir := t.TempDir()
		path := put(t, dir, name, document("ps_00000000000000000000000001", ""))

		_, err := LoadLayer(dir, LayerPersonal, LoadOptions{})

		assertError(t, err, path)
	}
	for _, kind := range []Kind{KindSkill, KindCommand} {
		out := t.TempDir()
		if _, err := WriteItem(out, Item{ID: NewID(), Kind: kind, Name: "Not A Slug"}); err == nil {
			t.Errorf("%s was written", kind)
		}
		if len(tree(t, out)) != 0 {
			t.Errorf("%s left files", kind)
		}
	}
}

func TestInvalidFilesAreRejected(t *testing.T) {
	const id = "ps_00000000000000000000000001"
	tests := map[string]struct{ file, content, mention string }{
		"skill name differs": {"skills/a/SKILL.md", "---\nname: b\npromptstor:\n  id: " + id + "\n---\n", "name"},
		"unclosed":           {"rules/a.md", "---\nname: a\n", "---"},
		"unknown block key":  {"rules/a.md", document(id, "  lock: true\n"), "lock"},
		"duplicate key":      {"rules/a.md", document(id, "model: a\nmodel: b\n"), "model"},
		"alias":              {"rules/a.md", document(id, "a: &x 1\nb: *x\n"), "alias"},
		"activation":         {"rules/a.md", document(id, "  activation: sometimes\n"), "sometimes"},
		"not a mapping":      {"mcp/a.yaml", "- a\n", "mapping"},
		"two documents":      {"mcp/a.yaml", "transport: a\n---\ntransport: b\n", "document"},
		"stray file":         {"mcp/a.yml", "transport: a\n", ".yaml"},
		"missing SKILL.md":   {"skills/a/notes.md", "x", "SKILL.md"},
		"orphan scripts":     {"hooks/a/run.sh", "x", "a.yaml"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			put(t, dir, test.file, test.content)

			_, err := LoadLayer(dir, LayerPersonal, LoadOptions{})

			assertError(t, err, filepath.Join(dir, filepath.FromSlash(strings.Split(test.file, "/")[0])), test.mention)
		})
	}
}

func TestLoadErrorsNeverShowValues(t *testing.T) {
	const secret = "synthetic-secret-value"
	for _, content := range []string{
		"env:\n  TOKEN:\n    value: " + secret + "\n",
		"env: " + secret + "\n",
		"args: " + secret + "\n",
		"resolve: " + secret + "\n",
		"headers:\n  A: [" + secret + "]\n",
	} {
		dir := t.TempDir()
		path := put(t, dir, "mcp/a.yaml", content)

		_, err := LoadLayer(dir, LayerPersonal, LoadOptions{AssignIDs: true})

		assertError(t, err, path)
		if err != nil && strings.Contains(err.Error(), secret[:7]) {
			t.Errorf("error shows a value from content %d bytes long", len(content))
		}
	}
}

func TestLoadSkipsMissingDirsAndDSStore(t *testing.T) {
	if items := load(t, filepath.Join(t.TempDir(), "missing")); len(items) != 0 {
		t.Errorf("items = %v", items)
	}
	dir := t.TempDir()
	put(t, dir, "rules/.DS_Store", "x")
	put(t, dir, "skills/.DS_Store", "x")
	put(t, dir, "skills/a/SKILL.md", document("ps_00000000000000000000000001", ""))
	put(t, dir, "skills/a/.DS_Store", "x")

	items := load(t, dir)

	if len(items) != 1 || len(items[0].Files) != 0 {
		t.Errorf("items = %+v", items)
	}
}

func TestSymlinksAreNeverFollowed(t *testing.T) {
	outside := t.TempDir()
	target := put(t, outside, "target.md", document("ps_00000000000000000000000001", ""))
	dir := t.TempDir()
	put(t, dir, "skills/a/SKILL.md", document("ps_00000000000000000000000002", ""))
	link := filepath.Join(dir, "skills", "a", "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	_, err := LoadLayer(dir, LayerPersonal, LoadOptions{})
	assertError(t, err, link, "symlink")

	skill := Item{ID: "ps_00000000000000000000000002", Kind: KindSkill, Name: "a"}
	if _, err := WriteItem(dir, skill); err == nil {
		t.Error("wrote a skill folder holding a symlink")
	}
	if err := DeleteItem(dir, skill); err == nil {
		t.Error("deleted a skill folder holding a symlink")
	}
	if got := read(t, filepath.Join(dir, "skills", "a", "SKILL.md")); got != document("ps_00000000000000000000000002", "") {
		t.Errorf("SKILL.md changed to %q", got)
	}
}

func TestWriteItemRefusesWhatItCannotStore(t *testing.T) {
	file := map[string]File{"a": {Data: []byte("x")}}
	tests := map[string]Item{
		"unknown kind":       {Kind: "other", Name: "a"},
		"no slug":            {Kind: KindRule, Name: "!!!"},
		"files on a rule":    {Kind: KindRule, Name: "a", Files: file},
		"native on a rule":   {Kind: KindRule, Name: "a", Native: map[string]any{"claude": 1}},
		"body on mcp":        {Kind: KindMCP, Name: "a", Body: "x"},
		"mcp on a rule":      {Kind: KindRule, Name: "a", MCP: &MCPServer{}},
		"portable on mcp":    {Kind: KindMCP, Name: "a", Portable: &PortablePermission{}},
		"reserved meta":      {Kind: KindRule, Name: "a", Meta: map[string]any{"promptstor": 1}},
		"escaping file":      {Kind: KindSkill, Name: "a", Files: map[string]File{"../b": {}}},
		"main file in Files": {Kind: KindSkill, Name: "a", Files: map[string]File{"SKILL.md": {}}},
		"literal and ref":    {Kind: KindMCP, Name: "a", MCP: &MCPServer{Env: map[string]Value{"A": {Literal: "x", Ref: "env:A"}}}},
		"bad activation":     {Kind: KindRule, Name: "a", Activation: "sometimes"},
	}
	for name, item := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			item.ID = NewID()

			if _, err := WriteItem(dir, item); err == nil {
				t.Error("no error")
			}
			if files := tree(t, dir); len(files) != 0 {
				t.Errorf("left %v", files)
			}
		})
	}
}

func TestWriteItemDropsStaleAttachments(t *testing.T) {
	dir := t.TempDir()
	skill := Item{ID: NewID(), Kind: KindSkill, Name: "a", Files: map[string]File{"old/x": {Data: []byte("x")}}}
	write(t, dir, skill)
	skill.Files = map[string]File{"new": {Data: []byte("y")}}

	path := write(t, dir, skill)

	if path != filepath.Join(dir, "skills", "a", "SKILL.md") {
		t.Errorf("path = %s", path)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "a", "old")); !os.IsNotExist(err) {
		t.Errorf("stale directory remains: %v", err)
	}
	if got := load(t, dir)[0].Files; len(got) != 1 || got["new"].Mode != 0o644 {
		t.Errorf("files = %v", got)
	}
}

func TestWriteItemKeepsTheLoadedFileName(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "rules/file_Name.md", document("ps_00000000000000000000000001", ""))
	item := load(t, dir)[0]
	item.Name = "Renamed rule"

	path := write(t, dir, item)

	if path != filepath.Join(dir, "rules", "file_Name.md") {
		t.Errorf("path = %s", path)
	}
	if got := load(t, dir); len(got) != 1 || got[0].Name != "Renamed rule" {
		t.Errorf("items = %+v", got)
	}
}

func TestDeleteItemRemovesEveryFile(t *testing.T) {
	dir := t.TempDir()
	for _, item := range load(t, fixture) {
		write(t, dir, item)
	}

	for _, item := range load(t, dir) {
		if err := DeleteItem(dir, item); err != nil {
			t.Fatal(err)
		}
	}

	if files := tree(t, dir); len(files) != 0 {
		t.Errorf("left %v", files)
	}
	for _, name := range []string{"skills/release-notes", "hooks/format-check"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s remains: %v", name, err)
		}
	}
}

func TestContentHash(t *testing.T) {
	built := Item{
		ID:    "ps_00000000000000000000000001",
		Kind:  KindSkill,
		Name:  "a",
		Body:  "body\n",
		Meta:  map[string]any{"b": []string{"x"}, "a": "line\nline"},
		Files: map[string]File{"run.sh": {Data: []byte("x"), Mode: 0o755}},
	}
	dir := t.TempDir()
	write(t, dir, built)
	loaded := load(t, dir)[0]
	hash := ContentHash(built)

	if len(hash) != 64 || ContentHash(loaded) != hash {
		t.Errorf("hash %q differs after a round trip: %q", hash, ContentHash(loaded))
	}
	moved := loaded
	moved.Source, moved.Layer = "/elsewhere", LayerTeam
	if ContentHash(moved) != hash {
		t.Error("Source or Layer changes the hash")
	}
	for name, change := range map[string]func(*Item){
		"body":    func(i *Item) { i.Body = "other" },
		"mode":    func(i *Item) { i.Files = map[string]File{"run.sh": {Data: []byte("x"), Mode: 0o644}} },
		"content": func(i *Item) { i.Files = map[string]File{"run.sh": {Data: []byte("y"), Mode: 0o755}} },
		"key":     func(i *Item) { i.Files = map[string]File{"other.sh": {Data: []byte("x"), Mode: 0o755}} },
	} {
		changed := built
		change(&changed)
		if ContentHash(changed) == hash {
			t.Errorf("%s does not change the hash", name)
		}
	}
}

func TestFileInPlaceOfItemFolderIsKept(t *testing.T) {
	for _, kind := range []Kind{KindSkill, KindHook} {
		dir := t.TempDir()
		name := map[Kind]string{KindSkill: "skills/a", KindHook: "hooks/a"}[kind]
		path := put(t, dir, name, "keep")

		_, err := WriteItem(dir, Item{ID: NewID(), Kind: kind, Name: "a"})

		assertError(t, err, path)
		if files := tree(t, dir); !reflect.DeepEqual(files, map[string]string{name: "keep"}) {
			t.Errorf("%s left %v", kind, files)
		}
	}
}

func TestWriteRefusesDifferentIDAtSamePath(t *testing.T) {
	const stored, other = "ps_00000000000000000000000001", "ps_00000000000000000000000002"
	for name, test := range map[string]struct {
		file, content string
		kind          Kind
	}{
		"rule":    {"rules/a.md", document(stored, ""), KindRule},
		"skill":   {"skills/a/SKILL.md", document(stored, ""), KindSkill},
		"hook":    {"hooks/a.yaml", "promptstor:\n  id: " + stored + "\n", KindHook},
		"no id":   {"rules/a.md", "body\n", KindRule},
		"damaged": {"rules/a.md", "---\nunclosed\n", KindRule},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := put(t, dir, test.file, test.content)
			item := Item{ID: other, Kind: test.kind, Name: "a"}
			if test.kind != KindRule {
				folder := strings.TrimSuffix(strings.TrimSuffix(test.file, "/SKILL.md"), ".yaml")
				put(t, dir, folder+"/keep.sh", "x")
				item.Files = map[string]File{"new.sh": {Data: []byte("y")}}
			}
			before := tree(t, dir)

			_, err := WriteItem(dir, item)

			switch name {
			case "damaged":
				assertError(t, err, path)
			case "no id":
				assertError(t, err, path, other)
			default:
				assertError(t, err, path, stored, other)
			}
			if after := tree(t, dir); !reflect.DeepEqual(after, before) {
				t.Errorf("layer changed to %q", after)
			}
		})
	}
}

func TestYAMLKindUnknownKeyRejected(t *testing.T) {
	for _, name := range []string{"permissions/a.yaml", "hooks/a.yaml", "mcp/a.yaml"} {
		dir := t.TempDir()
		content := "promptstor:\n  id: ps_00000000000000000000000001\ndeny_path:\n  - x\n"
		path := put(t, dir, name, content)

		_, err := LoadLayer(dir, LayerPersonal, LoadOptions{AssignIDs: true})

		assertError(t, err, path, "deny_path")
		if got := read(t, path); got != content {
			t.Errorf("%s changed to %q", name, got)
		}
	}
	for _, kind := range []Kind{KindPermission, KindHook, KindMCP} {
		dir := t.TempDir()
		item := Item{ID: NewID(), Kind: kind, Name: "a", Meta: map[string]any{"deny_path": []string{"x"}}}

		_, err := WriteItem(dir, item)

		assertError(t, err, "deny_path")
		if files := tree(t, dir); len(files) != 0 {
			t.Errorf("%s left %v", kind, files)
		}
	}
}

func TestDotfilesInSkillFolderIgnored(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "skills/a/SKILL.md", document("ps_00000000000000000000000001", ""))
	put(t, dir, "hooks/h.yaml", "promptstor:\n  id: ps_00000000000000000000000002\n")
	hidden := []string{"skills/a/.env", "skills/a/.cache/data", "skills/a/sub/.env", "hooks/h/.env"}
	for _, name := range hidden {
		put(t, dir, name, "synthetic")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "skills", "a", ".linked")); err != nil {
		t.Fatal(err)
	}
	assertHidden := func(step string) {
		t.Helper()
		for _, name := range hidden {
			if got := read(t, filepath.Join(dir, filepath.FromSlash(name))); got != "synthetic" {
				t.Errorf("%s changed %s", step, name)
			}
		}
	}

	items := load(t, dir)

	out := t.TempDir()
	for _, item := range items {
		if len(item.Files) != 0 {
			t.Errorf("%s loaded %d files", item.Name, len(item.Files))
		}
		write(t, out, item)
		write(t, dir, item)
	}
	if copied := tree(t, out); len(copied) != 2 {
		t.Errorf("copied %d files, want 2", len(copied))
	}
	assertHidden("write")
	for _, item := range items {
		if err := DeleteItem(dir, item); err != nil {
			t.Fatal(err)
		}
	}
	assertHidden("delete")
	if left := load(t, dir); len(left) != 0 {
		t.Errorf("items = %+v", left)
	}
	for _, key := range []string{".env", "sub/.env", ".cache/data"} {
		item := Item{ID: NewID(), Kind: KindSkill, Name: "b", Files: map[string]File{key: {}}}
		if _, err := WriteItem(t.TempDir(), item); err == nil {
			t.Errorf("wrote %s", key)
		}
	}
}

func TestContentHashChangesOnRenameAndKind(t *testing.T) {
	original := Item{ID: "ps_00000000000000000000000001", Kind: KindRule, Name: "one", Body: "body\n"}
	renamed, command := original, original
	renamed.Name = "two"
	command.Kind = KindCommand

	if ContentHash(renamed) == ContentHash(original) {
		t.Error("renaming a rule keeps the hash")
	}
	if ContentHash(command) == ContentHash(original) {
		t.Error("a rule and a command with the same ID, name and body share a hash")
	}
}
