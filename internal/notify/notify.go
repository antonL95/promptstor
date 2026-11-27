package notify

import (
	"errors"
	"fmt"
	"strings"

	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	title         = "PromptStor"
	inSyncMessage = "PromptStor: everything is in sync again"
)

type Runner func(name string, args ...string) ([]byte, error)

type Desktop struct {
	Run Runner
}

func (d Desktop) Notify(env paths.Env, r drift.Report) error {
	return OnChange(env, r, d.Run)
}

func OnChange(env paths.Env, r drift.Report, run Runner) error {
	previous, found, readErr := drift.ReadLastCheck(env)
	if err := drift.WriteLastCheck(env, r, r.Generated); err != nil {
		return errors.Join(readErr, err)
	}
	if readErr != nil {
		return readErr
	}
	message := changeMessage(previous, found, r)
	if message == "" {
		return nil
	}
	return send(env, message, run)
}

func changeMessage(previous drift.LastCheck, found bool, r drift.Report) string {
	switch {
	case found && previous.Fingerprint == r.Fingerprint():
		return ""
	case !r.Clean():
		return driftMessage(r.DriftCount())
	case found && !previous.Clean:
		return inSyncMessage
	default:
		return ""
	}
}

func driftMessage(count int) string {
	noun := "findings"
	if count == 1 {
		noun = "finding"
	}
	return fmt.Sprintf("PromptStor found %d drift %s. Run promptstor doctor.", count, noun)
}

func send(env paths.Env, message string, run Runner) error {
	var name string
	var args []string
	switch env.OS {
	case "darwin":
		name, args = "osascript", []string{"-e", appleScript(message)}
	case "linux":
		if !onPath(env, "notify-send") {
			return nil
		}
		name, args = "notify-send", []string{title, message}
	default:
		return nil
	}
	if _, err := run(name, args...); err != nil {
		return fmt.Errorf("notify with %s: %w", name, err)
	}
	return nil
}

func onPath(env paths.Env, name string) bool {
	if env.LookPath == nil {
		return false
	}
	_, err := env.LookPath(name)
	return err == nil
}

func appleScript(message string) string {
	return fmt.Sprintf(`display notification "%s" with title "%s"`, escapeAppleScript(message), title)
}

func escapeAppleScript(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
