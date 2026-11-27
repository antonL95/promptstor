package schedule

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
)

const (
	serviceUnit = "promptstor-check.service"
	timerUnit   = "promptstor-check.timer"
)

var serviceTemplate = template.Must(template.New("service").Funcs(template.FuncMap{"exec": quoteExec}).Parse(`[Unit]
Description=PromptStor drift check

[Service]
Type=oneshot
ExecStart=:{{exec .Binary}} doctor --check --notify
SuccessExitStatus=1
`))

var timerTemplate = template.Must(template.New("timer").Parse(`[Unit]
Description=Run the PromptStor drift check every hour

[Timer]
OnBootSec=2min
OnUnitActiveSec=1h
Persistent=true

[Install]
WantedBy=timers.target
`))

type systemd struct{ Scheduler }

func (s systemd) install() (string, error) {
	systemdErr := s.enableTimer()
	if systemdErr == nil {
		message := fmt.Sprintf("Scheduled an hourly check with a systemd user timer: %s", s.unitPath(timerUnit))
		if err := s.cron().remove(); err != nil {
			message += fmt.Sprintf("\nCould not remove an earlier crontab entry: %v", err)
		}
		return message, nil
	}
	var commandErr commandError
	if !errors.As(systemdErr, &commandErr) {
		return "", systemdErr
	}
	if err := s.removeUnits(); err != nil {
		return "", errors.Join(systemdErr, err)
	}
	if err := s.cron().install(); err != nil {
		return "", errors.Join(systemdErr, err)
	}
	return "Scheduled an hourly check in your crontab, because systemctl --user is not available.", nil
}

type commandError struct{ error }

func (e commandError) Unwrap() error { return e.error }

func (s systemd) enableTimer() error {
	data := struct{ Binary string }{s.Binary}
	if err := s.writeDefinition(s.unitPath(serviceUnit), serviceTemplate, data); err != nil {
		return err
	}
	if err := s.writeDefinition(s.unitPath(timerUnit), timerTemplate, nil); err != nil {
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "--now", timerUnit}} {
		if _, err := s.systemctl(args...); err != nil {
			return commandError{err}
		}
	}
	return nil
}

func (s systemd) remove() error {
	found, err := exists(s.unitPath(timerUnit))
	if err != nil {
		return err
	}
	if found {
		if _, err := s.systemctl("disable", "--now", timerUnit); err != nil {
			return err
		}
	}
	if err := s.removeUnits(); err != nil {
		return err
	}
	if found {
		if _, err := s.systemctl("daemon-reload"); err != nil {
			return err
		}
	}
	return s.cron().remove()
}

func (s systemd) status() (string, error) {
	path := s.unitPath(timerUnit)
	found, err := exists(path)
	if err != nil {
		return "", err
	}
	if found {
		state := "installed and active"
		if _, err := s.systemctl("is-active", timerUnit); err != nil {
			state = "installed but not active"
		}
		return fmt.Sprintf("systemd user timer %s: %s", state, path), nil
	}
	scheduled, err := s.cron().installed()
	if err != nil {
		return "", err
	}
	if scheduled {
		return "crontab entry installed and active", nil
	}
	return notInstalled, nil
}

func (s systemd) removeUnits() error {
	return errors.Join(s.removeDefinition(s.unitPath(timerUnit)), s.removeDefinition(s.unitPath(serviceUnit)))
}

func (s systemd) systemctl(args ...string) ([]byte, error) {
	return s.run("systemctl", append([]string{"--user"}, args...)...)
}

func (s systemd) unitPath(name string) string {
	return filepath.Join(s.Env.SystemdUserDir(), name)
}

func (s systemd) cron() cron { return cron(s) }

func quoteExec(path string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(path) + `"`
}
