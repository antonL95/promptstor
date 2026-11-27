package cli

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/testenv"
)

func firstRunHome(t *testing.T) Deps {
	t.Helper()
	env := cliHome(t)
	if err := os.Remove(env.ConfigFile()); err != nil {
		t.Fatal(err)
	}
	return deps(env)
}

func TestDoctorNeedsTerminalOrYes(t *testing.T) {
	d := firstRunHome(t)
	before := testenv.TreeHash(t, d.Env.Home)
	out, err := run(t, d, "doctor")
	if err == nil || !strings.Contains(err.Error(), "doctor needs a terminal or --yes") {
		t.Fatalf("doctor without a terminal: %v\n%s", err, out)
	}
	if testenv.TreeHash(t, d.Env.Home) != before {
		t.Error("doctor wrote before failing")
	}
}

func TestDoctorYesDryRunWritesNothing(t *testing.T) {
	d := firstRunHome(t)
	before := testenv.TreeHash(t, d.Env.Home)
	out := mustRun(t, d, "doctor", "--yes", "--dry-run")
	if testenv.TreeHash(t, d.Env.Home) != before {
		t.Fatalf("--dry-run wrote to the home\n%s", out)
	}
	if _, err := os.Stat(d.Env.StateDir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("--dry-run created the state dir: %v", err)
	}
	if !strings.Contains(out, "dry run: nothing was written") {
		t.Errorf("output:\n%s", out)
	}
}

func TestFirstRunJSON(t *testing.T) {
	d := firstRunHome(t)
	before := testenv.TreeHash(t, d.Env.Home)

	out := mustRun(t, d, "doctor", "--json", "--dry-run")

	type detectedTool struct {
		Tool      string   `json:"tool"`
		Installed bool     `json:"installed"`
		Paths     []string `json:"paths"`
	}
	var doc struct {
		Tools       []detectedTool   `json:"tools"`
		Comparison  []map[string]any `json:"comparison"`
		Planned     []map[string]any `json:"planned"`
		Blocked     []map[string]any `json:"blocked"`
		Unsupported []map[string]any `json:"unsupported"`
	}
	decoder := json.NewDecoder(strings.NewReader(out))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, out)
	}
	if decoder.More() {
		t.Fatalf("more than one JSON document:\n%s", out)
	}
	if !slices.ContainsFunc(doc.Tools, func(tool detectedTool) bool { return tool.Tool == "claude-code" && tool.Installed }) {
		t.Errorf("detection table lacks claude-code: %+v", doc.Tools)
	}
	if len(doc.Planned) == 0 {
		t.Errorf("no planned files:\n%s", out)
	}
	for _, prompt := range []string{"Which tool is your primary", "(--yes)", "dry run"} {
		if strings.Contains(out, prompt) {
			t.Errorf("JSON output holds %q:\n%s", prompt, out)
		}
	}
	if testenv.TreeHash(t, d.Env.Home) != before {
		t.Error("--json --dry-run wrote to the home")
	}

	if out, err := run(t, d, "doctor", "--json"); err == nil || !strings.Contains(err.Error(), "doctor --json needs --yes or --dry-run") {
		t.Errorf("--json alone: %v\n%s", err, out)
	}
	if testenv.TreeHash(t, d.Env.Home) != before {
		t.Error("--json alone wrote to the home")
	}
}

func TestDoctorCheckExitCode(t *testing.T) {
	d := deps(cliHome(t))
	out, err := run(t, d, "doctor", "--check", "--json")
	var code ExitCode
	if !errors.As(err, &code) || code != 1 {
		t.Fatalf("doctor --check on a home with pending changes: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"clean": false`) {
		t.Errorf("--json output:\n%s", out)
	}
	if _, err := os.Stat(d.Env.LastCheckPath()); err != nil {
		t.Errorf("last check not written: %v", err)
	}
}
