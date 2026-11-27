package testenv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func Golden(t testing.TB, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file, run with -update to create it: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}

func TreeHash(t testing.TB, root string) string {
	t.Helper()
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == root {
			return err
		}
		return hashEntry(hash, root, path, entry)
	})
	if err != nil {
		t.Fatalf("hash %s: %v", root, err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func hashEntry(hash io.Writer, root, path string, entry fs.DirEntry) error {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	info, err := entry.Info()
	if err != nil {
		return err
	}
	var detail string
	mode := info.Mode()
	switch {
	case mode&fs.ModeSymlink != 0:
		mode = fs.ModeSymlink
		if detail, err = os.Readlink(path); err != nil {
			return err
		}
	case mode.IsRegular():
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		detail = fmt.Sprintf("%x", sha256.Sum256(content))
	}
	_, err = fmt.Fprintf(hash, "%q %s %q\n", filepath.ToSlash(relative), mode, detail)
	return err
}
