package schedule

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/antonL95/promptstor/internal/fsx"
)

const (
	cronTag        = "# promptstor"
	pendingCrontab = "crontab.new"
)

var cronTimes = []string{"0 * * * *", "@reboot"}

type cron struct{ Scheduler }

func (c cron) install() error {
	if err := c.prepareLog(); err != nil {
		return err
	}
	current, err := c.read()
	if err != nil {
		return err
	}
	next, _ := withoutTagged(current)
	if next != "" && !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	command := fmt.Sprintf("%s doctor --check --notify >> %s 2>&1 %s", quoteCron(c.Binary), quoteCron(c.Env.CheckLogPath()), cronTag)
	for _, when := range cronTimes {
		next += when + " " + command + "\n"
	}
	if next == current {
		return nil
	}
	return c.write(next)
}

func (c cron) remove() error {
	current, err := c.read()
	if errors.Is(err, exec.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	next, found := withoutTagged(current)
	if !found {
		return nil
	}
	return c.write(next)
}

func (c cron) installed() (bool, error) {
	current, err := c.read()
	if errors.Is(err, exec.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, found := withoutTagged(current)
	return found, nil
}

func (c cron) read() (string, error) {
	out, err := c.run("crontab", "-l")
	if err != nil {
		if hasNoCrontab(err) {
			return "", nil
		}
		return "", err
	}
	return string(out), nil
}

func (c cron) write(content string) (err error) {
	path := filepath.Join(c.Env.StateDir(), pendingCrontab)
	if err := c.removeState(path); err != nil {
		return err
	}
	if err := fsx.WriteAtomic(c.Env.StateDir(), path, []byte(content), stateMode); err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, c.removeState(path))
	}()
	_, err = c.run("crontab", path)
	return err
}

func (c cron) removeState(path string) error {
	found, err := exists(path)
	if err != nil || !found {
		return err
	}
	return fsx.Remove(c.Env.StateDir(), path)
}

func hasNoCrontab(err error) bool {
	message := err.Error()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		message = string(exit.Stderr)
	}
	return strings.Contains(message, "no crontab for")
}

func withoutTagged(crontab string) (string, bool) {
	var kept bytes.Buffer
	found := false
	for _, line := range strings.SplitAfter(crontab, "\n") {
		if strings.HasSuffix(strings.TrimSuffix(line, "\n"), cronTag) {
			found = true
			continue
		}
		kept.WriteString(line)
	}
	return kept.String(), found
}

func quoteCron(path string) string {
	quoted := "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
	return strings.ReplaceAll(quoted, "%", `\%`)
}
