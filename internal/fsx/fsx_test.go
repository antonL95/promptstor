package fsx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func mustWrite(t *testing.T, path, content string, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func assertContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s holds %q, want %q", path, got, want)
	}
}

func assertEntries(t *testing.T, dir string, want int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != want {
		t.Errorf("%s holds %d entries, want %d: %v", dir, len(entries), want, entries)
	}
}

func TestWriteAtomicRefusesSymlinkFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.md")
	mustWrite(t, target, "original", 0o644)
	link := filepath.Join(root, "CLAUDE.md")
	mustSymlink(t, target, link)

	err := WriteAtomic(root, link, []byte("changed"), 0o644)

	if !errors.Is(err, ErrSymlink) {
		t.Fatalf("err = %v, want ErrSymlink", err)
	}
	assertContent(t, target, "original")
	assertEntries(t, root, 1)
}

func TestWriteAtomicRefusesSymlinkParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustSymlink(t, outside, filepath.Join(root, "skills", "x"))

	for _, path := range []string{
		filepath.Join(root, "skills", "x", "SKILL.md"),
		filepath.Join(root, "skills", "x", "deeper", "SKILL.md"),
	} {
		if err := WriteAtomic(root, path, []byte("changed"), 0o644); !errors.Is(err, ErrSymlink) {
			t.Errorf("WriteAtomic(%s) = %v, want ErrSymlink", path, err)
		}
	}
	assertEntries(t, outside, 0)
}

func TestWriteAtomicRefusesDanglingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "dangling")
	mustSymlink(t, filepath.Join(outside, "missing"), link)

	if err := WriteAtomic(root, link, []byte("changed"), 0o644); !errors.Is(err, ErrSymlink) {
		t.Fatalf("err = %v, want ErrSymlink", err)
	}
	assertEntries(t, outside, 0)
}

func TestWriteAtomicKeepsExistingMode(t *testing.T) {
	for _, mode := range []fs.FileMode{0o600, 0o755} {
		root := t.TempDir()
		path := filepath.Join(root, "settings.json")
		mustWrite(t, path, "old", mode)

		if err := WriteAtomic(root, path, []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("mode = %o, want %o", info.Mode().Perm(), mode)
		}
		assertContent(t, path, "new")
		assertEntries(t, root, 1)
	}
}

func TestWriteAtomicCreatesFileAndParents(t *testing.T) {
	tests := []struct {
		mode, wantDir fs.FileMode
	}{
		{0o600, 0o700},
		{0o644, 0o755},
	}
	for _, test := range tests {
		root := filepath.Join(t.TempDir(), "root")
		path := filepath.Join(root, "a", "b", "file")

		if err := WriteAtomic(root, path, []byte("new"), test.mode); err != nil {
			t.Fatal(err)
		}

		file, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if file.Mode().Perm() != test.mode {
			t.Errorf("file mode = %o, want %o", file.Mode().Perm(), test.mode)
		}
		dir, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if dir.Mode().Perm()&^test.wantDir != 0 {
			t.Errorf("dir mode = %o, want at most %o", dir.Mode().Perm(), test.wantDir)
		}
		assertContent(t, path, "new")
	}
}

func TestWriteAtomicLeavesNoTempFileOnFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "taken")
	mustWrite(t, filepath.Join(path, "child"), "x", 0o600)

	if err := WriteAtomic(root, path, []byte("new"), 0o600); err == nil {
		t.Fatal("writing over a directory succeeded")
	}
	assertEntries(t, root, 1)
}

func TestLinkedAncestor(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	mustWrite(t, filepath.Join(base, "real", "root", "plain", "file"), "x", 0o600)
	mustSymlink(t, filepath.Join(base, "real"), filepath.Join(base, "alias"))
	root := filepath.Join(base, "alias", "root")
	mustSymlink(t, outside, filepath.Join(root, "first"))
	mustSymlink(t, outside, filepath.Join(outside, "second"))
	mustSymlink(t, "file", filepath.Join(root, "plain", "link"))

	tests := []struct {
		name   string
		path   string
		want   string
		linked bool
	}{
		{"linked root and parents are allowed", filepath.Join(root, "plain", "file"), "", false},
		{"root itself", root, "", false},
		{"missing path", filepath.Join(root, "missing", "file"), "", false},
		{"linked file", filepath.Join(root, "plain", "link"), filepath.Join(root, "plain", "link"), true},
		{"outermost link wins", filepath.Join(root, "first", "second", "file"), filepath.Join(root, "first"), true},
		{"name starting with dots is inside", filepath.Join(root, "..notes"), "", false},
	}
	for _, test := range tests {
		got, linked, err := LinkedAncestor(root, test.path)
		if err != nil {
			t.Errorf("%s: %v", test.name, err)
			continue
		}
		if got != test.want || linked != test.linked {
			t.Errorf("%s: got %q %v, want %q %v", test.name, got, linked, test.want, test.linked)
		}
	}
}

func TestPathsOutsideRootAreRefused(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	sibling := filepath.Join(filepath.Dir(root), "root-sibling", "file")
	mustWrite(t, sibling, "original", 0o600)

	for _, path := range []string{sibling, filepath.Dir(root), root + "/../root-sibling/file", "relative"} {
		if _, _, err := LinkedAncestor(root, path); err == nil {
			t.Errorf("LinkedAncestor(%s) succeeded", path)
		}
		if err := WriteAtomic(root, path, []byte("changed"), 0o600); err == nil {
			t.Errorf("WriteAtomic(%s) succeeded", path)
		}
		if err := Remove(root, path); err == nil {
			t.Errorf("Remove(%s) succeeded", path)
		}
	}
	assertContent(t, sibling, "original")
}

func TestRemoveRefusesSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "file")
	mustWrite(t, target, "original", 0o600)
	mustSymlink(t, outside, filepath.Join(root, "dir"))
	mustSymlink(t, target, filepath.Join(root, "link"))

	for _, path := range []string{filepath.Join(root, "dir", "file"), filepath.Join(root, "link")} {
		if err := Remove(root, path); !errors.Is(err, ErrSymlink) {
			t.Errorf("Remove(%s) = %v, want ErrSymlink", path, err)
		}
	}
	assertContent(t, target, "original")
	assertEntries(t, root, 2)
}

func TestRemoveDeletesFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	mustWrite(t, path, "x", 0o600)

	if err := Remove(root, path); err != nil {
		t.Fatal(err)
	}
	assertEntries(t, root, 0)
	if err := Remove(root, path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("second Remove = %v, want fs.ErrNotExist", err)
	}
}

func TestInspect(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	mustWrite(t, file, "x", 0o640)
	link := filepath.Join(root, "link")
	mustSymlink(t, "missing", link)

	missing, err := Inspect(filepath.Join(root, "missing"))
	if err != nil || missing != (Entry{}) {
		t.Errorf("missing = %+v, %v", missing, err)
	}
	got, err := Inspect(file)
	if err != nil || !got.Exists || got.IsDir || got.IsLink || got.Mode.Perm() != 0o640 {
		t.Errorf("file = %+v, %v", got, err)
	}
	got, err = Inspect(root)
	if err != nil || !got.Exists || !got.IsDir || got.IsLink {
		t.Errorf("dir = %+v, %v", got, err)
	}
	got, err = Inspect(link)
	if err != nil || !got.Exists || !got.IsLink || got.IsDir || got.LinkTarget != "missing" {
		t.Errorf("link = %+v, %v", got, err)
	}
	if _, err := Inspect(filepath.Join(file, "child")); err == nil {
		t.Error("inspecting below a file succeeded")
	}
}

func TestRootItselfIsNeverWrittenOrRemoved(t *testing.T) {
	target := t.TempDir()
	root := filepath.Join(t.TempDir(), "root")
	mustSymlink(t, target, root)

	if err := WriteAtomic(root, root, []byte("changed"), 0o600); err == nil {
		t.Error("WriteAtomic replaced the root")
	}
	if err := Remove(root, root+string(filepath.Separator)); err == nil {
		t.Error("Remove deleted the root")
	}
	entry, err := Inspect(root)
	if err != nil || !entry.IsLink {
		t.Errorf("root = %+v, %v", entry, err)
	}
}
