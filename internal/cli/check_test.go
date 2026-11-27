package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckExitCodes(t *testing.T) {
	tests := map[string]struct {
		args    []string
		arrange func(t *testing.T, root string)
		code    int
		output  string
	}{
		"in sync":    {[]string{"--ci"}, func(*testing.T, string) {}, 0, "in sync\n"},
		"difference": {[]string{"--ci"}, func(t *testing.T, root string) { removeFile(t, root, "AGENTS.md") }, 1, "missing: AGENTS.md"},
		"no layer":   {[]string{"--ci"}, func(t *testing.T, root string) { removeFile(t, root, ".promptstor") }, 2, "no .promptstor/config.yaml"},
		"without ci": {nil, func(*testing.T, string) {}, 2, "only check --ci"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS("../ci/testdata/repo")); err != nil {
				t.Fatal(err)
			}
			test.arrange(t, root)
			var out bytes.Buffer
			cmd := NewRoot(Deps{Stdout: &out, Stderr: &out, Stdin: strings.NewReader("")})
			cmd.SetArgs(append([]string{"check", "--dir", root}, test.args...))

			err := cmd.Execute()

			var exit ExitCode
			switch {
			case test.code == 0 && err != nil:
				t.Fatalf("error %v, output:\n%s", err, out.String())
			case test.code != 0 && (!errors.As(err, &exit) || int(exit) != test.code):
				t.Fatalf("error %v, output:\n%s", err, out.String())
			}
			if !strings.Contains(out.String(), test.output) {
				t.Errorf("output does not contain %q:\n%s", test.output, out.String())
			}
		})
	}
}

func TestCheckDefaultsToTheWorkingDirectory(t *testing.T) {
	t.Chdir("../ci/testdata/repo")
	cmd := NewRoot(Deps{Stdout: io.Discard, Stderr: io.Discard, Stdin: strings.NewReader("")})
	cmd.SetArgs([]string{"check", "--ci"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func removeFile(t *testing.T, root, name string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
}
