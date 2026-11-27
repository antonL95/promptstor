package paths

import (
	"fmt"
	"path/filepath"
	"strings"
)

const appDir = "promptstor"

type Env struct {
	Home       string
	ConfigHome string
	StateHome  string
	OS         string
	Getenv     func(string) string
	LookPath   func(string) (string, error)
}

func (e Env) WithBaseDirs() Env {
	if e.OS == "windows" {
		e.ConfigHome = e.variable("APPDATA")
		e.StateHome = e.variable("LOCALAPPDATA")
		return e
	}
	e.ConfigHome = e.absoluteVariableOr("XDG_CONFIG_HOME", filepath.Join(e.Home, ".config"))
	e.StateHome = e.absoluteVariableOr("XDG_STATE_HOME", filepath.Join(e.Home, ".local", "state"))
	return e
}

func (e Env) Validate() error {
	for _, dir := range []struct{ name, path string }{
		{"home directory", e.Home},
		{"config directory", e.ConfigHome},
		{"state directory", e.StateHome},
	} {
		if !filepath.IsAbs(dir.path) {
			return fmt.Errorf("%s %q is not an absolute path", dir.name, dir.path)
		}
	}
	return nil
}

func (e Env) Expand(p string) string {
	if p == "~" {
		return e.Home
	}
	if rest, found := strings.CutPrefix(p, "~/"); found {
		return filepath.Join(e.Home, rest)
	}
	return p
}

func (e Env) LibraryDir() string { return e.configPath("library") }

func (e Env) DeviceDir() string { return e.configPath("device") }

func (e Env) ConfigFile() string { return e.configPath("config.yaml") }

func (e Env) HooksDir() string { return e.configPath("hooks") }

func (e Env) StateDir() string { return filepath.Join(e.StateHome, appDir) }

func (e Env) ManifestPath() string { return e.statePath("manifest.json") }

func (e Env) AcksPath() string { return e.statePath("acks.json") }

func (e Env) LastCheckPath() string { return e.statePath("last-check.json") }

func (e Env) LockPath() string { return e.statePath("lock") }

func (e Env) BackupDir() string { return e.statePath("backups") }

func (e Env) WorktreeDir() string { return e.statePath("worktrees") }

func (e Env) CheckLogPath() string { return e.statePath("check.log") }

func (e Env) LaunchAgentPath(label string) string {
	return filepath.Join(e.Home, "Library", "LaunchAgents", label+".plist")
}

func (e Env) SystemdUserDir() string { return filepath.Join(e.Home, ".config", "systemd", "user") }

func (e Env) configPath(name string) string { return filepath.Join(e.ConfigHome, appDir, name) }

func (e Env) statePath(name string) string { return filepath.Join(e.StateDir(), name) }

func (e Env) variable(name string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(name)
}

func (e Env) absoluteVariableOr(name, fallback string) string {
	if value := e.variable(name); filepath.IsAbs(value) {
		return value
	}
	return fallback
}
