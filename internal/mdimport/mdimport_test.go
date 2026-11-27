package mdimport_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/mdimport"
	"github.com/antonL95/promptstor/internal/paths"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func env(home string) paths.Env {
	return paths.Env{Home: home, Getenv: func(string) string { return "" }}
}

func resolve(t *testing.T, e paths.Env, path string, syn mdimport.Syntax) mdimport.Resolved {
	t.Helper()
	resolved, err := mdimport.Resolve(e, path, syn)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func files(root string, resolved mdimport.Resolved) []string {
	var found []string
	for _, f := range resolved.Files {
		relative, _ := filepath.Rel(root, f.Path)
		found = append(found, filepath.ToSlash(relative)+"@"+string(rune('0'+f.Depth)))
	}
	return found
}

func relativeUnresolved(root string, resolved mdimport.Resolved) []adapter.Unresolved {
	var found []adapter.Unresolved
	for _, u := range resolved.Unresolved {
		relative, _ := filepath.Rel(root, u.File)
		u.File = filepath.ToSlash(relative)
		found = append(found, u)
	}
	return found
}

func TestResolveClaudeDepthFour(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"CLAUDE.md": "# Top\n@one.md\n",
		"one.md":    "@two.md\n",
		"two.md":    "@three.md\n",
		"three.md":  "@four.md\n",
		"four.md":   "@five.md\n",
		"five.md":   "Too deep.\n",
	})
	resolved := resolve(t, env(root), filepath.Join(root, "CLAUDE.md"), mdimport.Claude)
	want := []string{"CLAUDE.md@0", "one.md@1", "two.md@2", "three.md@3", "four.md@4"}
	if got := files(root, resolved); !reflect.DeepEqual(got, want) {
		t.Errorf("files %v", got)
	}
	wantUnresolved := []adapter.Unresolved{{File: "four.md", Line: "1", Target: "five.md", Reason: "depth"}}
	if got := relativeUnresolved(root, resolved); !reflect.DeepEqual(got, wantUnresolved) {
		t.Errorf("unresolved %+v", got)
	}
	if string(resolved.Files[1].Body) != "@two.md\n" {
		t.Errorf("body %q", resolved.Files[1].Body)
	}
}

func TestResolveSkipsCodeFences(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"CLAUDE.md": "See `@inline.md` here.\n```\n@fenced.md\n```\n~~~md\n@tilde-fenced.md\n~~~\nSee @real.md and mail me at someone@example.com\n",
		"real.md":   "Real.\n",
	})
	resolved := resolve(t, env(root), filepath.Join(root, "CLAUDE.md"), mdimport.Claude)
	if got := files(root, resolved); !reflect.DeepEqual(got, []string{"CLAUDE.md@0", "real.md@1"}) {
		t.Errorf("files %v", got)
	}
	if len(resolved.Unresolved) != 0 {
		t.Errorf("unresolved %+v", resolved.Unresolved)
	}
}

func TestResolveMissingReported(t *testing.T) {
	home := t.TempDir()
	writeFiles(t, home, map[string]string{
		".claude/CLAUDE.md":           "@missing.md\n@~/notes/present.md\n@\"quoted file.md\"\n@Design\\ Docs/spec.md\n@~/notes/absent.md\n",
		"notes/present.md":            "Present.\n",
		".claude/Design Docs/spec.md": "Spec.\n",
	})
	resolved := resolve(t, env(home), filepath.Join(home, ".claude", "CLAUDE.md"), mdimport.Claude)
	want := []string{".claude/CLAUDE.md@0", "notes/present.md@1", ".claude/Design Docs/spec.md@1"}
	if got := files(home, resolved); !reflect.DeepEqual(got, want) {
		t.Errorf("files %v", got)
	}
	wantUnresolved := []adapter.Unresolved{
		{File: ".claude/CLAUDE.md", Line: "1", Target: "missing.md", Reason: "missing"},
		{File: ".claude/CLAUDE.md", Line: "3", Target: "\"quoted", Reason: "syntax"},
		{File: ".claude/CLAUDE.md", Line: "5", Target: "~/notes/absent.md", Reason: "missing"},
	}
	if got := relativeUnresolved(home, resolved); !reflect.DeepEqual(got, wantUnresolved) {
		t.Errorf("unresolved %+v", got)
	}
}

func TestResolveCycleStops(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"CLAUDE.md": "@a.md\n@shared.md\n",
		"a.md":      "@b.md\n@shared.md\n",
		"b.md":      "@a.md\n",
		"shared.md": "Shared.\n",
	})
	resolved := resolve(t, env(root), filepath.Join(root, "CLAUDE.md"), mdimport.Claude)
	want := []string{"CLAUDE.md@0", "a.md@1", "b.md@2", "shared.md@2"}
	if got := files(root, resolved); !reflect.DeepEqual(got, want) {
		t.Errorf("files %v", got)
	}
	wantUnresolved := []adapter.Unresolved{{File: "b.md", Line: "1", Target: "a.md", Reason: "cycle"}}
	if got := relativeUnresolved(root, resolved); !reflect.DeepEqual(got, wantUnresolved) {
		t.Errorf("unresolved %+v", got)
	}
}

func TestResolveGeminiRejectsTilde(t *testing.T) {
	home := t.TempDir()
	writeFiles(t, home, map[string]string{
		".gemini/GEMINI.md": "@~/notes/style.md\n@./local.md\n@-flag\n",
		".gemini/local.md":  "Local.\n",
		"notes/style.md":    "Never read.\n",
	})
	resolved := resolve(t, env(home), filepath.Join(home, ".gemini", "GEMINI.md"), mdimport.Gemini)
	if got := files(home, resolved); !reflect.DeepEqual(got, []string{".gemini/GEMINI.md@0", ".gemini/local.md@1"}) {
		t.Errorf("files %v", got)
	}
	want := []adapter.Unresolved{
		{File: ".gemini/GEMINI.md", Line: "1", Target: "~/notes/style.md", Reason: "tilde"},
		{File: ".gemini/GEMINI.md", Line: "3", Target: "-flag", Reason: "syntax"},
	}
	if got := relativeUnresolved(home, resolved); !reflect.DeepEqual(got, want) {
		t.Errorf("unresolved %+v", got)
	}
}

func TestResolveGeminiConfinedToGitRoot(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"repo/.git/HEAD":  "ref: refs/heads/main\n",
		"repo/GEMINI.md":  "@docs/a.md\n@../outside.md\n",
		"repo/docs/a.md":  "@../../outside.md\n@/" + filepath.ToSlash(strings.TrimPrefix(filepath.Join(root, "repo", "b.md"), "/")) + "\n",
		"repo/b.md":       "B.\n",
		"outside.md":      "Never read.\n",
		"plain/GEMINI.md": "@sub/c.md\n@../outside.md\n",
		"plain/sub/c.md":  "C.\n",
	})
	resolved := resolve(t, env(root), filepath.Join(root, "repo", "GEMINI.md"), mdimport.Gemini)
	if got := files(root, resolved); !reflect.DeepEqual(got, []string{"repo/GEMINI.md@0", "repo/docs/a.md@1", "repo/b.md@2"}) {
		t.Errorf("files %v", got)
	}
	want := []adapter.Unresolved{
		{File: "repo/docs/a.md", Line: "1", Target: "../../outside.md", Reason: "outside git root"},
		{File: "repo/GEMINI.md", Line: "2", Target: "../outside.md", Reason: "outside git root"},
	}
	if got := relativeUnresolved(root, resolved); !reflect.DeepEqual(got, want) {
		t.Errorf("unresolved %+v", got)
	}
	plain := resolve(t, env(root), filepath.Join(root, "plain", "GEMINI.md"), mdimport.Gemini)
	if got := files(root, plain); !reflect.DeepEqual(got, []string{"plain/GEMINI.md@0", "plain/sub/c.md@1"}) {
		t.Errorf("files without a git root %v", got)
	}
	if len(plain.Unresolved) != 1 || plain.Unresolved[0].Reason != "outside git root" {
		t.Errorf("unresolved without a git root %+v", plain.Unresolved)
	}
}

func TestResolveNeverReadsCredentialFiles(t *testing.T) {
	home := t.TempDir()
	writeFiles(t, home, map[string]string{
		".claude/CLAUDE.md":              "@~/.codex/auth.json\n@linked.md\n@~/.local/share/opencode/notes.md\n",
		".codex/auth.json":               "fixture-secret-value\n",
		".local/share/opencode/notes.md": "fixture-secret-value\n",
	})
	if err := os.Symlink("../.codex/auth.json", filepath.Join(home, ".claude", "linked.md")); err != nil {
		t.Fatal(err)
	}
	resolved := resolve(t, env(home), filepath.Join(home, ".claude", "CLAUDE.md"), mdimport.Claude)
	if got := files(home, resolved); !reflect.DeepEqual(got, []string{".claude/CLAUDE.md@0"}) {
		t.Errorf("files %v", got)
	}
	want := []adapter.Unresolved{
		{File: ".claude/CLAUDE.md", Line: "1", Target: "~/.codex/auth.json", Reason: "credential file"},
		{File: ".claude/CLAUDE.md", Line: "2", Target: "linked.md", Reason: "credential file"},
		{File: ".claude/CLAUDE.md", Line: "3", Target: "~/.local/share/opencode/notes.md", Reason: "credential file"},
	}
	if got := relativeUnresolved(home, resolved); !reflect.DeepEqual(got, want) {
		t.Errorf("unresolved %+v", got)
	}
}

func TestResolveAntigravityInclude(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	writeFiles(t, outside, map[string]string{"absolute.md": "Absolute.\n"})
	absolute := filepath.Join(outside, "absolute.md")
	writeFiles(t, home, map[string]string{
		".gemini/config/rules/main.md": "@[Relative](../shared/relative.md)\nSee @[Home](~/notes/home.md) and @[Absolute](" + absolute + ").\n" +
			"Not an include: @plain.md and [a link](link.md) and `@[Code](code.md)`.\n```\n@[Fenced](fenced.md)\n```\n",
		".gemini/config/shared/relative.md":      "@[Nested](deeper/nested.md)\n",
		".gemini/config/shared/deeper/nested.md": "Nested.\n",
		"notes/home.md":                          "Home.\n",
	})
	resolved := resolve(t, env(home), filepath.Join(home, ".gemini", "config", "rules", "main.md"), mdimport.Antigravity)
	var got []string
	for _, f := range resolved.Files {
		got = append(got, f.Path)
	}
	want := []string{
		filepath.Join(home, ".gemini", "config", "rules", "main.md"),
		filepath.Join(home, ".gemini", "config", "shared", "relative.md"),
		filepath.Join(home, ".gemini", "config", "shared", "deeper", "nested.md"),
		filepath.Join(home, "notes", "home.md"),
		absolute,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("files %v", got)
	}
	if len(resolved.Unresolved) != 0 {
		t.Errorf("unresolved %+v", resolved.Unresolved)
	}
}

func TestResolveAntigravityMissingAndCycle(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"main.md":  "@[Gone](gone.md)\n@[A](a.md)\n@[Remote](https://example.com/x.md)\n@[Empty]()\n",
		"a.md":     "@[Back](main.md)\n",
		"one.md":   "@[2](two.md)\n",
		"two.md":   "@[3](three.md)\n",
		"three.md": "@[4](four.md)\n",
		"four.md":  "@[5](five.md)\n",
		"five.md":  "@[6](six.md)\n",
		"six.md":   "@[7](seven.md)\n",
		"seven.md": "Too deep.\n",
	})
	resolved := resolve(t, env(root), filepath.Join(root, "main.md"), mdimport.Antigravity)
	if got := files(root, resolved); !reflect.DeepEqual(got, []string{"main.md@0", "a.md@1"}) {
		t.Errorf("files %v", got)
	}
	want := []adapter.Unresolved{
		{File: "main.md", Line: "1", Target: "[Gone](gone.md)", Reason: "missing"},
		{File: "a.md", Line: "1", Target: "[Back](main.md)", Reason: "cycle"},
		{File: "main.md", Line: "3", Target: "[Remote](https://example.com/x.md)", Reason: "syntax"},
		{File: "main.md", Line: "4", Target: "[Empty]()", Reason: "syntax"},
	}
	if got := relativeUnresolved(root, resolved); !reflect.DeepEqual(got, want) {
		t.Errorf("unresolved %+v", got)
	}
	deep := resolve(t, env(root), filepath.Join(root, "one.md"), mdimport.Antigravity)
	wantDeep := []adapter.Unresolved{{File: "six.md", Line: "1", Target: "[7](seven.md)", Reason: "depth"}}
	if got := relativeUnresolved(root, deep); len(deep.Files) != 6 || !reflect.DeepEqual(got, wantDeep) {
		t.Errorf("files %v unresolved %+v", files(root, deep), got)
	}
}

func TestResolveRootMissingIsAnError(t *testing.T) {
	root := t.TempDir()
	if _, err := mdimport.Resolve(env(root), filepath.Join(root, "CLAUDE.md"), mdimport.Claude); err == nil {
		t.Error("no error for a missing root file")
	}
}

func TestStripImportLines(t *testing.T) {
	doc := "@AGENTS.md\n## Rules\n\nSee @guide.md for details.\n  @one.md @two.md  \n```\n@fenced.md\n```\n"
	want := "## Rules\n\nSee @guide.md for details.\n```\n@fenced.md\n```\n"
	if got := string(mdimport.StripImportLines([]byte(doc), mdimport.Claude)); got != want {
		t.Errorf("stripped %q", got)
	}
	cases := map[string]string{
		"Intro.\n\n@a.md\n@b.md\n\nMore.\n": "Intro.\n\nMore.\n",
		"@a.md\n\n## Rules\n":               "## Rules\n",
		"Intro.\n\n@a.md\n":                 "Intro.\n\n",
	}
	for doc, want := range cases {
		if got := string(mdimport.StripImportLines([]byte(doc), mdimport.Claude)); got != want {
			t.Errorf("stripped %q to %q, want %q", doc, got, want)
		}
	}
}
