package schedule_test

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/schedule"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	uid      = 501
	binary   = "/opt/promptstor/bin/promptstor"
	userCron = "MAILTO=\"\"\n\n# nightly jobs\n0 1 * * *  /usr/local/bin/backup   # not # promptstor-related\n"
)

type machine struct {
	t         *testing.T
	calls     []string
	failures  map[string]error
	crontab   string
	noCrontab bool
}

func (m *machine) run(name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	m.calls = append(m.calls, call)
	for prefix, err := range m.failures {
		if strings.HasPrefix(call, prefix) {
			return nil, err
		}
	}
	if name != "crontab" {
		return nil, nil
	}
	if args[0] == "-l" {
		if m.noCrontab {
			return nil, &exec.ExitError{Stderr: []byte("no crontab for tester\n")}
		}
		return []byte(m.crontab), nil
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		m.t.Errorf("crontab was given an unreadable file: %v", err)
		return nil, err
	}
	m.crontab, m.noCrontab = string(data), false
	return nil, nil
}

func (m *machine) writes() int {
	count := 0
	for _, call := range m.calls {
		if strings.HasPrefix(call, "crontab ") && call != "crontab -l" {
			count++
		}
	}
	return count
}

func scheduler(t *testing.T, os string) (schedule.Scheduler, *machine) {
	t.Helper()
	env := testenv.Home(t, t.TempDir())
	env.OS = os
	m := &machine{t: t, failures: map[string]error{}, noCrontab: true}
	return schedule.Scheduler{Env: env, Binary: binary, Run: m.run, Getuid: func() int { return uid }}, m
}

func plistPath(env paths.Env) string {
	return filepath.Join(env.Home, "Library", "LaunchAgents", "com.promptstor.check.plist")
}

func unitPath(env paths.Env, name string) string {
	return filepath.Join(env.Home, ".config", "systemd", "user", name)
}

func install(t *testing.T, s schedule.Scheduler) string {
	t.Helper()
	message, err := s.Install()
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func remove(t *testing.T, s schedule.Scheduler) {
	t.Helper()
	if err := s.Remove(); err != nil {
		t.Fatal(err)
	}
}

func status(t *testing.T, s schedule.Scheduler) string {
	t.Helper()
	message, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func golden(t *testing.T, env paths.Env, path, name string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	testenv.Golden(t, filepath.Join("testdata", "golden", name), []byte(strings.ReplaceAll(string(data), env.Home, "HOME")))
}

func assertCalls(t *testing.T, m *machine, want ...string) {
	t.Helper()
	if !slices.Equal(m.calls, want) {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(m.calls, "\n"), strings.Join(want, "\n"))
	}
	m.calls = nil
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s still exists: %v", path, err)
	}
}

func TestLaunchdPlistGolden(t *testing.T) {
	s, m := scheduler(t, "darwin")
	s.Binary = "/opt/Prompt & <Stor>/promptstor"

	message := install(t, s)

	path := plistPath(s.Env)
	golden(t, s.Env, path, "com.promptstor.check.plist")
	if !strings.Contains(message, path) {
		t.Errorf("message %q does not name %s", message, path)
	}
	log, err := os.Stat(filepath.Join(s.Env.StateDir(), "check.log"))
	if err != nil || log.Mode().Perm() != 0o600 {
		t.Errorf("check.log: %v %v", log, err)
	}
	reload := []string{"launchctl bootout gui/501/com.promptstor.check", "launchctl bootstrap gui/501 " + path}
	assertCalls(t, m, reload...)

	writeFile(t, filepath.Join(s.Env.StateDir(), "check.log"), "earlier run\n")
	writeFile(t, path, "stale")
	install(t, s)
	golden(t, s.Env, path, "com.promptstor.check.plist")
	assertCalls(t, m, reload...)
	if data, err := os.ReadFile(filepath.Join(s.Env.StateDir(), "check.log")); err != nil || string(data) != "earlier run\n" {
		t.Errorf("check.log was rewritten: %q %v", data, err)
	}
}

func TestLaunchdLoadFailureIsReported(t *testing.T) {
	s, m := scheduler(t, "darwin")
	m.failures["launchctl bootstrap"] = errors.New("exit status 5")
	if _, err := s.Install(); err == nil || !strings.Contains(err.Error(), "launchctl bootstrap") {
		t.Fatalf("install: %v", err)
	}
}

func TestSystemdUnitsGolden(t *testing.T) {
	s, m := scheduler(t, "linux")
	s.Binary = `/opt/a b/100%/$HOME/back\slash/prompt"stor`

	install(t, s)

	golden(t, s.Env, unitPath(s.Env, "promptstor-check.service"), "promptstor-check.service")
	golden(t, s.Env, unitPath(s.Env, "promptstor-check.timer"), "promptstor-check.timer")
	reload := []string{"systemctl --user daemon-reload", "systemctl --user enable --now promptstor-check.timer", "crontab -l"}
	assertCalls(t, m, reload...)

	install(t, s)
	assertCalls(t, m, reload...)
}

func TestSystemdInstallDropsEarlierCronLines(t *testing.T) {
	s, m := scheduler(t, "linux")
	m.crontab, m.noCrontab = userCron+"0 * * * * '/old/promptstor' doctor --check --notify # promptstor\n", false

	install(t, s)

	if m.crontab != userCron {
		t.Errorf("crontab %q", m.crontab)
	}
}

func TestSystemdInstallReportsAnUnreadableCrontab(t *testing.T) {
	s, m := scheduler(t, "linux")
	m.failures["crontab -l"] = errors.New("not allowed to use this program")

	message := install(t, s)

	if !strings.Contains(message, "systemd user timer") || !strings.Contains(message, "not allowed to use this program") {
		t.Errorf("message %q", message)
	}
}

func TestCronFallbackWhenSystemctlFails(t *testing.T) {
	s, m := scheduler(t, "linux")
	s.Binary = "/opt/it's 100%/promptstor"
	m.failures["systemctl"] = errors.New("exit status 1")
	m.crontab, m.noCrontab = userCron, false

	message := install(t, s)

	command := `'/opt/it'\''s 100\%/promptstor' doctor --check --notify >> '` + filepath.Join(s.Env.StateDir(), "check.log") + `' 2>&1 # promptstor`
	want := userCron + "0 * * * * " + command + "\n@reboot " + command + "\n"
	if m.crontab != want {
		t.Errorf("crontab:\n%s\nwant:\n%s", m.crontab, want)
	}
	if !strings.Contains(message, "crontab") {
		t.Errorf("message %q", message)
	}
	assertMissing(t, unitPath(s.Env, "promptstor-check.service"))
	assertMissing(t, unitPath(s.Env, "promptstor-check.timer"))
	left, err := os.ReadDir(s.Env.StateDir())
	if err != nil || len(left) != 1 || left[0].Name() != "check.log" {
		t.Errorf("state dir holds %v %v, want only check.log", left, err)
	}
	if got := status(t, s); !strings.Contains(got, "crontab") {
		t.Errorf("status %q", got)
	}

	install(t, s)
	if m.crontab != want {
		t.Errorf("second install:\n%s", m.crontab)
	}

	remove(t, s)
	if m.crontab != userCron {
		t.Errorf("crontab after remove %q, want %q", m.crontab, userCron)
	}
}

func TestCronFallbackStartsAnAbsentCrontab(t *testing.T) {
	s, m := scheduler(t, "linux")
	m.failures["systemctl"] = errors.New("exit status 1")

	install(t, s)

	if lines := strings.Split(strings.TrimSuffix(m.crontab, "\n"), "\n"); len(lines) != 2 {
		t.Errorf("crontab %q", m.crontab)
	}
}

func TestCronFallbackKeepsAnUnterminatedLastLine(t *testing.T) {
	s, m := scheduler(t, "linux")
	m.failures["systemctl"] = errors.New("exit status 1")
	m.crontab, m.noCrontab = "5 4 * * * true", false

	install(t, s)

	if !strings.HasPrefix(m.crontab, "5 4 * * * true\n0 * * * * ") {
		t.Errorf("crontab %q", m.crontab)
	}
}

func TestUnreadableCrontabIsNeverOverwritten(t *testing.T) {
	s, m := scheduler(t, "linux")
	m.failures["systemctl"] = errors.New("exit status 1")
	m.failures["crontab -l"] = errors.New("permission denied")
	m.crontab, m.noCrontab = userCron, false

	if _, err := s.Install(); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("install: %v", err)
	}
	if err := s.Remove(); err == nil {
		t.Error("remove ignored an unreadable crontab")
	}
	if m.writes() != 0 || m.crontab != userCron {
		t.Errorf("crontab was written: %q", m.crontab)
	}
}

func TestRemoveIsIdempotent(t *testing.T) {
	for _, os := range []string{"darwin", "linux"} {
		t.Run(os, func(t *testing.T) {
			s, m := scheduler(t, os)
			notLoaded := errors.New("exit status 3")
			m.failures["launchctl bootout"] = notLoaded
			m.failures["launchctl print"] = notLoaded

			remove(t, s)
			install(t, s)
			remove(t, s)
			remove(t, s)

			assertMissing(t, plistPath(s.Env))
			assertMissing(t, unitPath(s.Env, "promptstor-check.service"))
			assertMissing(t, unitPath(s.Env, "promptstor-check.timer"))
			if m.writes() != 0 {
				t.Errorf("an untouched crontab was rewritten: %v", m.calls)
			}
		})
	}
}

func TestRemoveWithoutSystemdOrCrontab(t *testing.T) {
	s, m := scheduler(t, "linux")
	m.failures["systemctl"] = errors.New("exit status 1")
	m.failures["crontab"] = &exec.Error{Name: "crontab", Err: exec.ErrNotFound}
	remove(t, s)
}

func TestRemoveKeepsFilesOfAScheduleThatStaysActive(t *testing.T) {
	cases := map[string]struct {
		failing string
		path    func(paths.Env) string
	}{
		"darwin": {"launchctl bootout", plistPath},
		"linux": {"systemctl --user disable", func(env paths.Env) string {
			return unitPath(env, "promptstor-check.timer")
		}},
	}
	for os_, tc := range cases {
		t.Run(os_, func(t *testing.T) {
			s, m := scheduler(t, os_)
			install(t, s)
			m.failures[tc.failing] = errors.New("exit status 1")
			if err := s.Remove(); err == nil {
				t.Fatal("remove succeeded")
			}
			if _, err := os.Stat(tc.path(s.Env)); err != nil {
				t.Errorf("file removed: %v", err)
			}
		})
	}
}

func TestStatusFollowsInstallAndRemove(t *testing.T) {
	for _, os := range []string{"darwin", "linux"} {
		t.Run(os, func(t *testing.T) {
			s, m := scheduler(t, os)
			if got := status(t, s); got != "Scheduled checks are not installed." {
				t.Errorf("before install %q", got)
			}
			install(t, s)
			if got := status(t, s); !strings.Contains(got, "installed and active") {
				t.Errorf("after install %q", got)
			}
			m.failures["launchctl print"] = errors.New("exit status 113")
			m.failures["systemctl --user is-active"] = errors.New("exit status 3")
			if got := status(t, s); !strings.Contains(got, "installed but not active") {
				t.Errorf("unloaded %q", got)
			}
			delete(m.failures, "launchctl print")
			delete(m.failures, "systemctl --user is-active")
			remove(t, s)
			if got := status(t, s); got != "Scheduled checks are not installed." {
				t.Errorf("after remove %q", got)
			}
		})
	}
}

func TestInstallNeedsAnAbsoluteBinaryPath(t *testing.T) {
	for _, path := range []string{"", "promptstor", "/opt/two\nlines/promptstor"} {
		for _, os := range []string{"darwin", "linux"} {
			s, m := scheduler(t, os)
			s.Binary = path
			if _, err := s.Install(); err == nil || !strings.Contains(err.Error(), "binary") {
				t.Errorf("%s %q: %v", os, path, err)
			}
			if len(m.calls) != 0 {
				t.Errorf("%s %q ran %v", os, path, m.calls)
			}
		}
	}
}

func TestScheduleNeverWritesThroughASymlink(t *testing.T) {
	for os_, linked := range map[string]string{"darwin": "Library", "linux": ".config"} {
		t.Run(os_, func(t *testing.T) {
			s, m := scheduler(t, os_)
			target := t.TempDir()
			if err := os.Symlink(target, filepath.Join(s.Env.Home, linked)); err != nil {
				t.Fatal(err)
			}
			before := testenv.TreeHash(t, target)
			if _, err := s.Install(); !errors.Is(err, fsx.ErrSymlink) {
				t.Fatalf("install: %v", err)
			}
			if testenv.TreeHash(t, target) != before || len(m.calls) != 0 {
				t.Errorf("wrote through the link or ran %v", m.calls)
			}
		})
	}
}

func TestWindowsDeferred(t *testing.T) {
	s, m := scheduler(t, "windows")
	s.Binary = ""
	before := testenv.TreeHash(t, s.Env.Home)
	const want = "Scheduled checks on Windows arrive in release 2."

	if got := install(t, s); got != want {
		t.Errorf("install %q", got)
	}
	if got := status(t, s); got != want {
		t.Errorf("status %q", got)
	}
	remove(t, s)

	if len(m.calls) != 0 || testenv.TreeHash(t, s.Env.Home) != before {
		t.Errorf("windows ran %v", m.calls)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
