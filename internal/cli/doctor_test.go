package cli

import (
	"errors"
	"os"
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
