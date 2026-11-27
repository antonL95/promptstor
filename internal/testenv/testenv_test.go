package testenv

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".claude", "CLAUDE.md"), "rules", 0o600)
	write(t, filepath.Join(dir, "bin", "hook"), "#!/bin/sh\n", 0o755)
	link(t, ".claude", filepath.Join(dir, "linked"))
	return dir
}

func TestHomeCopiesFixtureTree(t *testing.T) {
	source := fixture(t)

	env := Home(t, source)

	if env.Home == source {
		t.Fatal("Home returned the fixture itself")
	}
	if got, want := TreeHash(t, env.Home), TreeHash(t, source); got != want {
		t.Error("the copy differs from the fixture")
	}
	target, err := os.Readlink(filepath.Join(env.Home, "linked"))
	if err != nil || target != ".claude" {
		t.Errorf("link target = %q, %v", target, err)
	}
}

func TestHomeDescribesAnIsolatedDarwinMachine(t *testing.T) {
	env := Home(t, fixture(t))

	if env.OS != "darwin" {
		t.Errorf("OS = %q", env.OS)
	}
	if want := filepath.Join(env.Home, ".config"); env.ConfigHome != want {
		t.Errorf("ConfigHome = %q, want %q", env.ConfigHome, want)
	}
	if want := filepath.Join(env.Home, ".local", "state"); env.StateHome != want {
		t.Errorf("StateHome = %q, want %q", env.StateHome, want)
	}
	if got := env.Getenv("PATH"); got != "" {
		t.Errorf("Getenv leaks the real environment: %d bytes", len(got))
	}
	if _, err := env.LookPath("sh"); !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("LookPath = %v, want exec.ErrNotFound", err)
	}
}

func TestCopyRefusesLinksLeavingTheTree(t *testing.T) {
	for name, target := range map[string]string{
		"absolute": t.TempDir(),
		"relative": "../outside",
	} {
		source := t.TempDir()
		link(t, target, filepath.Join(source, "escape"))

		if err := copyTree(source, t.TempDir()); err == nil {
			t.Errorf("%s link was copied", name)
		}
	}
}

func TestTreeHashSeesEveryChange(t *testing.T) {
	changes := map[string]func(t *testing.T, dir string){
		"content": func(t *testing.T, dir string) { write(t, filepath.Join(dir, "bin", "hook"), "other", 0o755) },
		"mode": func(t *testing.T, dir string) {
			write(t, filepath.Join(dir, "bin", "hook"), "#!/bin/sh\n", 0o700)
		},
		"new file": func(t *testing.T, dir string) { write(t, filepath.Join(dir, "extra"), "", 0o600) },
		"new dir": func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "empty"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"link target": func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "linked")); err != nil {
				t.Fatal(err)
			}
			link(t, "bin", filepath.Join(dir, "linked"))
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t)
			before := TreeHash(t, dir)
			if again := TreeHash(t, dir); again != before {
				t.Fatal("TreeHash is not stable")
			}

			change(t, dir)

			if TreeHash(t, dir) == before {
				t.Error("TreeHash did not change")
			}
		})
	}
}

func TestGoldenRewritesOnlyWithUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "golden", "file")
	previous := flag.Lookup("update").Value.String()
	t.Cleanup(func() {
		if err := flag.Set("update", previous); err != nil {
			t.Error(err)
		}
	})

	if err := flag.Set("update", "true"); err != nil {
		t.Fatal(err)
	}
	Golden(t, path, []byte("first"))
	if err := flag.Set("update", "false"); err != nil {
		t.Fatal(err)
	}
	Golden(t, path, []byte("first"))

	got, err := os.ReadFile(path)
	if err != nil || string(got) != "first" {
		t.Errorf("golden = %q, %v", got, err)
	}
}
