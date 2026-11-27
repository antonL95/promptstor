package cli

import (
	"errors"
	"strings"
	"testing"
)

func recordingDeps(t *testing.T) (Deps, *[]string) {
	t.Helper()
	d := deps(cliHome(t))
	calls := &[]string{}
	d.Run = func(name string, args ...string) ([]byte, error) {
		if name != "osascript" && name != "launchctl" {
			return nil, errors.New("unexpected command " + name)
		}
		*calls = append(*calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	return d, calls
}

func TestDoctorNotifiesOnlyOnChange(t *testing.T) {
	d, calls := recordingDeps(t)
	for range 2 {
		var code ExitCode
		if out, err := run(t, d, "doctor", "--check", "--notify"); !errors.As(err, &code) || code != 1 {
			t.Fatalf("doctor --check --notify: %v\n%s", err, out)
		}
	}
	if len(*calls) != 1 || !strings.Contains((*calls)[0], "Run promptstor doctor.") {
		t.Errorf("notifications %q", *calls)
	}
}

func TestDoctorNotifyDryRunStaysQuiet(t *testing.T) {
	d, calls := recordingDeps(t)
	_, _ = run(t, d, "doctor", "--check", "--notify", "--dry-run")
	if len(*calls) != 0 {
		t.Errorf("notifications %q", *calls)
	}
}

func TestScheduleCommands(t *testing.T) {
	d, calls := recordingDeps(t)
	d.Binary = guardBinary

	if out := mustRun(t, d, "schedule", "status"); out != "Scheduled checks are not installed.\n" {
		t.Errorf("status %q", out)
	}
	if out := mustRun(t, d, "schedule", "install"); !strings.Contains(out, "com.promptstor.check.plist") {
		t.Errorf("install %q", out)
	}
	if out := mustRun(t, d, "schedule", "status"); !strings.Contains(out, "installed and active") {
		t.Errorf("status %q", out)
	}
	if out := mustRun(t, d, "schedule", "remove"); out != "Scheduled checks removed.\n" {
		t.Errorf("remove %q", out)
	}
	if len(*calls) == 0 {
		t.Error("launchctl was not called through Deps.Run")
	}
}

func TestScheduleInstallWithoutBinaryFails(t *testing.T) {
	d, calls := recordingDeps(t)
	out, err := run(t, d, "schedule", "install")
	if err == nil || !strings.Contains(err.Error(), "path of the promptstor binary") {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if len(*calls) != 0 {
		t.Errorf("ran %q", *calls)
	}
}

func TestScheduleOnWindowsIsDeferred(t *testing.T) {
	d, calls := recordingDeps(t)
	d.Env.OS = "windows"
	for _, action := range []string{"install", "remove", "status"} {
		if out := mustRun(t, d, "schedule", action); out != "Scheduled checks on Windows arrive in release 2.\n" {
			t.Errorf("%s %q", action, out)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("ran %q", *calls)
	}
}
