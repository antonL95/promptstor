package notify_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/antonL95/promptstor/internal/doctor"
	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/notify"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/testenv"
)

var _ doctor.Notifier = notify.Desktop{}

type recorder struct {
	calls [][]string
	err   error
}

func (r *recorder) run(name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil, r.err
}

func (r *recorder) take() [][]string {
	calls := r.calls
	r.calls = nil
	return calls
}

func home(t *testing.T, os string) paths.Env {
	t.Helper()
	env := testenv.Home(t, t.TempDir())
	env.OS = os
	return env
}

func report(minute int, findings ...drift.Finding) drift.Report {
	return drift.Report{Generated: time.Date(2026, 11, 27, 10, minute, 0, 0, time.UTC), Findings: findings}
}

func finding(kind drift.FindingKind, name string) drift.Finding {
	return drift.Finding{Kind: kind, Name: name, Path: "/synthetic/" + name, Detail: "changed"}
}

func osascript(message string) [][]string {
	return [][]string{{"osascript", "-e", `display notification "` + message + `" with title "PromptStor"`}}
}

func TestNotifyOnlyWhenFingerprintChanges(t *testing.T) {
	env := home(t, "darwin")
	r := &recorder{}
	pending, edited, info := finding(drift.KindPending, "style"), finding(drift.KindHandEdit, "tests"), finding(drift.KindInfo, "skill")
	steps := []struct {
		name   string
		report drift.Report
		want   [][]string
	}{
		{"first check is clean", report(0), nil},
		{"drift appears", report(1, pending, info), osascript("PromptStor found 1 drift finding. Run promptstor doctor.")},
		{"same drift", report(2, info, pending), nil},
		{"more drift", report(3, pending, edited, info), osascript("PromptStor found 2 drift findings. Run promptstor doctor.")},
		{"back in sync", report(4, info), osascript("PromptStor: everything is in sync again")},
		{"clean stays quiet", report(5), nil},
	}
	for _, step := range steps {
		if err := notify.OnChange(env, step.report, r.run); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if got := r.take(); !slices.EqualFunc(got, step.want, slices.Equal) {
			t.Errorf("%s: ran %q, want %q", step.name, got, step.want)
		}
		last, found, err := drift.ReadLastCheck(env)
		if err != nil || !found || last.Fingerprint != step.report.Fingerprint() || !last.Checked.Equal(step.report.Generated) {
			t.Errorf("%s: last check %+v %v %v", step.name, last, found, err)
		}
	}
}

func TestDesktopNotifies(t *testing.T) {
	env := home(t, "darwin")
	r := &recorder{}
	if err := (notify.Desktop{Run: r.run}).Notify(env, report(0, finding(drift.KindMissing, "style"))); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || strings.Contains(strings.Join(r.calls[0], " "), "synthetic") {
		t.Errorf("ran %q", r.calls)
	}
}

func TestLinuxNotifySend(t *testing.T) {
	env := home(t, "linux")
	env.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	r := &recorder{}
	if err := notify.OnChange(env, report(0, finding(drift.KindPending, "style")), r.run); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"notify-send", "PromptStor", "PromptStor found 1 drift finding. Run promptstor doctor."}}
	if !slices.EqualFunc(r.calls, want, slices.Equal) {
		t.Errorf("ran %q", r.calls)
	}
}

func TestSkipsSilentlyWithoutNotifier(t *testing.T) {
	for _, os := range []string{"linux", "windows"} {
		env := home(t, os)
		r := &recorder{}
		if err := notify.OnChange(env, report(0, finding(drift.KindPending, "style")), r.run); err != nil {
			t.Fatalf("%s: %v", os, err)
		}
		if _, found, err := drift.ReadLastCheck(env); err != nil || !found || len(r.calls) != 0 {
			t.Errorf("%s: ran %q, found %v %v", os, r.calls, found, err)
		}
	}
}

func TestFailedNotificationStillRecordsTheCheck(t *testing.T) {
	env := home(t, "darwin")
	r := &recorder{err: errors.New("exit status 1")}
	err := notify.OnChange(env, report(0, finding(drift.KindPending, "style")), r.run)
	if err == nil || !strings.Contains(err.Error(), "osascript") {
		t.Fatalf("error %v", err)
	}
	if _, found, err := drift.ReadLastCheck(env); err != nil || !found {
		t.Errorf("last check: %v %v", found, err)
	}
}

func TestDamagedLastCheckIsReportedAndReplaced(t *testing.T) {
	env := home(t, "darwin")
	if err := os.MkdirAll(filepath.Dir(env.LastCheckPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.LastCheckPath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	if err := notify.OnChange(env, report(0), r.run); err == nil {
		t.Error("damaged last check was not reported")
	}
	if _, found, err := drift.ReadLastCheck(env); err != nil || !found {
		t.Errorf("last check not replaced: %v %v", found, err)
	}
}
