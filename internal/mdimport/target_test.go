package mdimport_test

import (
	"path/filepath"
	"testing"

	"github.com/antonL95/promptstor/internal/mdimport"
)

func TestOnlyImports(t *testing.T) {
	cases := map[string]bool{
		"@~/notes/review.md":          true,
		"  @a.md @b.md ":              true,
		"See @a.md for details":       false,
		"":                            false,
		"`@a.md`":                     false,
		"plain text without imports.": false,
	}
	for line, want := range cases {
		if got := mdimport.OnlyImports(line, mdimport.Claude); got != want {
			t.Errorf("OnlyImports(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestTargetPath(t *testing.T) {
	home := t.TempDir()
	from := filepath.Join(home, ".claude", "CLAUDE.md")
	cases := []struct {
		path, want string
	}{
		{"~/notes/review.md", filepath.Join(home, "notes", "review.md")},
		{"rules/style.md", filepath.Join(home, ".claude", "rules", "style.md")},
		{"/etc/example.md", "/etc/example.md"},
		{"https://example.com/x.md", ""},
	}
	for _, c := range cases {
		got, ok := mdimport.TargetPath(env(home), from, mdimport.Import{Path: c.path}, mdimport.Claude)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("TargetPath(%q) = %q, %v; want %q", c.path, got, ok, c.want)
		}
	}
}
