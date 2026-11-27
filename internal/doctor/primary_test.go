package doctor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/doctor"
)

type primaryProbe struct {
	*script
	labels []string
	def    int
}

func (p *primaryProbe) Select(q string, opts []string, def int) (int, error) {
	if strings.Contains(q, choosePrimary) && p.labels == nil {
		p.labels, p.def = opts, def
	}
	return p.script.Select(q, opts, def)
}

func probePrimary(t *testing.T, prepare func(home string)) *primaryProbe {
	t.Helper()
	env := fixtureHome(t)
	if err := os.MkdirAll(filepath.Join(env.Home, ".gemini", "antigravity"), 0o700); err != nil {
		t.Fatal(err)
	}
	prepare(env.Home)
	p := &primaryProbe{script: &script{}}
	run(t, env, p, doctor.Options{DryRun: true})
	if p.labels == nil {
		t.Fatal("primary question not asked")
	}
	return p
}

func label(labels []string, tool string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, tool+" (") {
			return l
		}
	}
	return ""
}

func TestPrimaryDefaultCountsOwnItems(t *testing.T) {
	t.Run("shared file does not count", func(t *testing.T) {
		var rules strings.Builder
		for _, name := range []string{"One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine"} {
			rules.WriteString("## " + name + "\n\nRule " + name + ".\n\n")
		}
		p := probePrimary(t, func(home string) {
			write(t, filepath.Join(home, "notes", "gemini.md"), rules.String())
		})
		if got := p.labels[p.def]; !strings.HasPrefix(got, "gemini-cli (") {
			t.Errorf("default %q in %v", got, p.labels)
		}
		if got := label(p.labels, "antigravity"); got != "antigravity (0 items)" {
			t.Errorf("antigravity label %q", got)
		}
		if got := label(p.labels, "gemini-cli"); !strings.HasPrefix(got, "gemini-cli (9 items") {
			t.Errorf("gemini-cli label %q", got)
		}
	})
	t.Run("ties follow the tool order", func(t *testing.T) {
		p := probePrimary(t, func(home string) {
			for _, dir := range []string{".claude", ".codex", ".gemini"} {
				if err := os.RemoveAll(filepath.Join(home, dir)); err != nil {
					t.Fatal(err)
				}
			}
			for _, dir := range []string{".claude", ".codex", ".gemini/antigravity"} {
				if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
					t.Fatal(err)
				}
			}
		})
		if got := p.labels[p.def]; got != "claude-code (0 items)" {
			t.Errorf("default %q in %v", got, p.labels)
		}
	})
}
