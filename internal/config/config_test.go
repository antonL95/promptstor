package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/config"
)

func TestConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "promptstor", "config.yaml")
	want := config.Config{
		Version:          1,
		Primary:          "claude-code",
		Tools:            []string{"claude-code", "codex"},
		FirstRunSnapshot: "20261127T100000Z-ab12",
		Adopted:          []string{"/home/example/.claude/CLAUDE.md"},
		ReplaceSymlinks:  []string{"/home/example/.codex/AGENTS.md"},
		EditGuards:       true,
		RegisterMCP:      true,
		OpenProjectPRs:   true,
		ResolveSecrets:   []string{"example-docs"},
	}
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	got, found, err := config.Load(path)
	if err != nil || !found {
		t.Fatalf("load: found %v err %v", found, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
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
	for _, key := range []string{"version:", "primary:", "tools:", "first_run_snapshot:", "adopted:", "replace_symlinks:", "edit_guards:", "register_mcp:", "open_project_prs:", "resolve_secrets:"} {
		if !strings.Contains(string(data), key) {
			t.Errorf("%s missing from\n%s", key, data)
		}
	}
}

func TestConfigMissingFileIsDefault(t *testing.T) {
	got, found, err := config.Load(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil || found {
		t.Fatalf("found %v err %v", found, err)
	}
	if !reflect.DeepEqual(got, config.Config{Version: 1}) {
		t.Errorf("got %+v", got)
	}
}

func TestConfigRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: 1\ntool: [codex]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("err %v", err)
	}
}

func TestConfigRejectsUnknownVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := config.Load(path); err == nil {
		t.Error("version 2 accepted")
	}
}

func TestLoadProjectReadsTools(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".promptstor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".promptstor", "config.yaml"), []byte("tools:\n  - codex\n  - cursor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, found, err := config.LoadProject(root)
	if err != nil || !found {
		t.Fatalf("found %v err %v", found, err)
	}
	if !reflect.DeepEqual(got.Tools, []string{"codex", "cursor"}) {
		t.Errorf("tools %v", got.Tools)
	}
	if _, found, err := config.LoadProject(t.TempDir()); found || err != nil {
		t.Errorf("empty project: found %v err %v", found, err)
	}
}
