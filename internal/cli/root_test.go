package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestVersionPrintsVersion(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRoot(Deps{Stdout: &out, Stderr: io.Discard, Stdin: strings.NewReader("")})
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "promptstor dev\n" {
		t.Fatalf("got %q", out.String())
	}
}
