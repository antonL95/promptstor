package archguard

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const environmentReader = "internal/paths/fromos.go"

var forbiddenLookups = []string{
	`os.UserHomeDir`,
	`os.UserConfigDir`,
	`os.UserCacheDir`,
	`Getenv("HOME")`,
	`LookupEnv("HOME")`,
	`Getenv("USERPROFILE")`,
	`Getenv("APPDATA")`,
	`Getenv("LOCALAPPDATA")`,
	`Getenv("XDG_`,
	`LookupEnv("XDG_`,
}

var (
	toolDirective   = regexp.MustCompile(`^//go:(build|generate|embed)([ \t]|$)`)
	lintSuppression = regexp.MustCompile(`^//nolint:[a-z0-9_-]+(,[a-z0-9_-]+)*$`)
	generatedMarker = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)
	blanketLint     = regexp.MustCompile(`[:,]all(,|$)`)
)

var skippedDirs = []string{".git", ".superpowers", "vendor"}

func TestNoHomeLookupsOutsideCmd(t *testing.T) {
	violations, err := homeLookups(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

func TestHomeLookupsAllowOnlyFromOS(t *testing.T) {
	lookups := []string{
		`os.UserHomeDir()`,
		`os.UserConfigDir()`,
		`os.UserCacheDir()`,
		`os.Getenv("HOME")`,
		`os.LookupEnv("HOME")`,
		`os.Getenv("USERPROFILE")`,
		`os.Getenv("APPDATA")`,
		`os.Getenv("LOCALAPPDATA")`,
		`os.Getenv("XDG_CONFIG_HOME")`,
		`os.LookupEnv("XDG_STATE_HOME")`,
	}
	source := "package planted\n" + strings.Join(lookups, "\n") + "\n"
	repo := t.TempDir()
	for _, name := range []string{
		"internal/paths/fromos.go",
		"internal/paths/other.go",
		"internal/nested/internal/paths/fromos.go",
	} {
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	violations, err := homeLookups(repo)
	if err != nil {
		t.Fatal(err)
	}

	report := strings.Join(violations, "\n")
	for _, name := range []string{"internal/paths/other.go", "internal/nested/internal/paths/fromos.go"} {
		for index, lookup := range lookups {
			location := fmt.Sprintf("%s:%d:", filepath.Join(repo, filepath.FromSlash(name)), index+2)
			if !strings.Contains(report, location) {
				t.Errorf("%s in %s was not reported", lookup, name)
			}
		}
	}
	if allowed := filepath.Join(repo, "internal", "paths", "fromos.go") + ":"; strings.Contains(report, allowed) {
		t.Error("internal/paths/fromos.go was reported")
	}
}

func TestNoCommentsInGoSources(t *testing.T) {
	violations, err := forbiddenComments(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

func homeLookups(repoRoot string) ([]string, error) {
	var violations []string
	err := walkGoFiles(filepath.Join(repoRoot, "internal"), func(path string) error {
		relative, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return fmt.Errorf("relate %s to %s: %w", path, repoRoot, err)
		}
		if strings.HasSuffix(path, "_test.go") || filepath.ToSlash(relative) == environmentReader {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		for index, line := range strings.Split(string(source), "\n") {
			for _, lookup := range forbiddenLookups {
				if strings.Contains(line, lookup) {
					violations = append(violations, fmt.Sprintf("%s:%d: forbidden %s", path, index+1, lookup))
				}
			}
		}
		return nil
	})
	return violations, err
}

func forbiddenComments(root string) ([]string, error) {
	var violations []string
	err := walkGoFiles(root, func(path string) error {
		files := token.NewFileSet()
		file, err := parser.ParseFile(files, path, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if !isDirective(comment.Text) {
					violations = append(violations, fmt.Sprintf("%s:%d: forbidden comment", path, files.Position(comment.Pos()).Line))
				}
			}
		}
		return nil
	})
	return violations, err
}

func isDirective(comment string) bool {
	if lintSuppression.MatchString(comment) {
		return !blanketLint.MatchString(comment)
	}
	return toolDirective.MatchString(comment) || generatedMarker.MatchString(comment)
}

func walkGoFiles(root string, visit func(path string) error) error {
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if slices.Contains(skippedDirs, entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		return visit(path)
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", root, err)
	}
	return nil
}
