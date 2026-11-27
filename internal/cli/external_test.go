package cli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/testenv"
)

func TestExternalCommand(t *testing.T) {
	d := deps(cliHome(t))
	agents := filepath.Join(d.Env.Home, ".codex", "AGENTS.md")
	skills := filepath.Join(d.Env.Home, ".claude", "skills")
	homeBefore := testenv.TreeHash(t, filepath.Join(d.Env.Home, ".claude"))

	if out := mustRun(t, d, "external", "list"); out != "No external paths.\n" {
		t.Errorf("empty list: %q", out)
	}
	mustRun(t, d, "external", "add", "~/.codex/AGENTS.md")
	out := mustRun(t, d, "external", "add", skills+"/")
	if want := skills + "\n" + agents + "\n"; out != want {
		t.Errorf("after add:\n%s\nwant:\n%s", out, want)
	}
	if out := mustRun(t, d, "external", "add", agents); out != skills+"\n"+agents+"\n" {
		t.Errorf("adding twice:\n%s", out)
	}
	cfg, _, err := config.Load(d.Env.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{skills, agents}; !reflect.DeepEqual(cfg.External, want) {
		t.Errorf("config external %v, want %v", cfg.External, want)
	}
	if !reflect.DeepEqual(cfg.Tools, []string{"claude-code", "codex"}) {
		t.Errorf("tools changed: %v", cfg.Tools)
	}
	if out := mustRun(t, d, "external", "remove", "~/.claude/skills"); out != agents+"\n" {
		t.Errorf("after remove:\n%s", out)
	}
	if _, err := run(t, d, "external", "remove", skills); err == nil || !strings.Contains(err.Error(), "is not an external path") {
		t.Errorf("removing an unknown path: %v", err)
	}
	if _, err := run(t, d, "external", "add", "relative/path"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("relative path: %v", err)
	}
	if testenv.TreeHash(t, filepath.Join(d.Env.Home, ".claude")) != homeBefore {
		t.Error("external changed a tool file")
	}
}

func TestExternalNeedsConfig(t *testing.T) {
	d := firstRunHome(t)
	before := testenv.TreeHash(t, d.Env.Home)

	_, err := run(t, d, "external", "add", "~/.codex/AGENTS.md")

	if err == nil || !strings.Contains(err.Error(), "run promptstor doctor first") {
		t.Errorf("external before the first run: %v", err)
	}
	if testenv.TreeHash(t, d.Env.Home) != before {
		t.Error("external wrote before the first run")
	}
}
