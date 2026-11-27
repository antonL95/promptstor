package paths

import (
	"path/filepath"
	"runtime"
	"testing"
)

func getenvFrom(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestExpandTilde(t *testing.T) {
	env := Env{Home: "/home/u"}
	tests := []struct{ in, want string }{
		{"~", "/home/u"},
		{"~/", "/home/u"},
		{"~/a/b", filepath.Join("/home/u", "a", "b")},
		{"~other/a", "~other/a"},
		{"/etc/~/a", "/etc/~/a"},
		{"a/b", "a/b"},
		{"", ""},
	}
	for _, test := range tests {
		if got := env.Expand(test.in); got != test.want {
			t.Errorf("Expand(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestConfigHomeFollowsXDG(t *testing.T) {
	tests := []struct {
		name        string
		variables   map[string]string
		wantLibrary string
		wantState   string
	}{
		{
			name:        "set",
			variables:   map[string]string{"XDG_CONFIG_HOME": "/x", "XDG_STATE_HOME": "/s"},
			wantLibrary: "/x/promptstor/library",
			wantState:   "/s/promptstor",
		},
		{
			name:        "unset",
			variables:   map[string]string{},
			wantLibrary: "/home/u/.config/promptstor/library",
			wantState:   "/home/u/.local/state/promptstor",
		},
		{
			name:        "relative is ignored",
			variables:   map[string]string{"XDG_CONFIG_HOME": "x", "XDG_STATE_HOME": "./s"},
			wantLibrary: "/home/u/.config/promptstor/library",
			wantState:   "/home/u/.local/state/promptstor",
		},
	}
	for _, goos := range []string{"linux", "darwin"} {
		for _, test := range tests {
			t.Run(goos+" "+test.name, func(t *testing.T) {
				env := Env{Home: "/home/u", OS: goos, Getenv: getenvFrom(test.variables)}.WithBaseDirs()
				if got := env.LibraryDir(); got != filepath.FromSlash(test.wantLibrary) {
					t.Errorf("LibraryDir() = %q, want %q", got, test.wantLibrary)
				}
				if got := env.StateDir(); got != filepath.FromSlash(test.wantState) {
					t.Errorf("StateDir() = %q, want %q", got, test.wantState)
				}
			})
		}
	}
}

func TestWindowsDirs(t *testing.T) {
	appData := `C:\Users\u\AppData\Roaming`
	localAppData := `C:\Users\u\AppData\Local`
	env := Env{
		Home: `C:\Users\u`,
		OS:   "windows",
		Getenv: getenvFrom(map[string]string{
			"APPDATA":         appData,
			"LOCALAPPDATA":    localAppData,
			"XDG_CONFIG_HOME": "/x",
			"XDG_STATE_HOME":  "/s",
		}),
	}.WithBaseDirs()

	if got, want := env.ConfigFile(), filepath.Join(appData, "promptstor", "config.yaml"); got != want {
		t.Errorf("ConfigFile() = %q, want %q", got, want)
	}
	if got, want := env.StateDir(), filepath.Join(localAppData, "promptstor"); got != want {
		t.Errorf("StateDir() = %q, want %q", got, want)
	}
}

func TestPathMethods(t *testing.T) {
	env := Env{ConfigHome: "/c", StateHome: "/s"}
	tests := []struct{ name, got, want string }{
		{"LibraryDir", env.LibraryDir(), "/c/promptstor/library"},
		{"DeviceDir", env.DeviceDir(), "/c/promptstor/device"},
		{"ConfigFile", env.ConfigFile(), "/c/promptstor/config.yaml"},
		{"HooksDir", env.HooksDir(), "/c/promptstor/hooks"},
		{"StateDir", env.StateDir(), "/s/promptstor"},
		{"ManifestPath", env.ManifestPath(), "/s/promptstor/manifest.json"},
		{"AcksPath", env.AcksPath(), "/s/promptstor/acks.json"},
		{"LastCheckPath", env.LastCheckPath(), "/s/promptstor/last-check.json"},
		{"LockPath", env.LockPath(), "/s/promptstor/lock"},
		{"BackupDir", env.BackupDir(), "/s/promptstor/backups"},
		{"WorktreeDir", env.WorktreeDir(), "/s/promptstor/worktrees"},
	}
	for _, test := range tests {
		if test.got != filepath.FromSlash(test.want) {
			t.Errorf("%s() = %q, want %q", test.name, test.got, test.want)
		}
	}
}

func TestValidateRejectsRelativeBaseDirs(t *testing.T) {
	absolute := t.TempDir()
	tests := []struct {
		name  string
		env   Env
		valid bool
	}{
		{"all absolute", Env{Home: absolute, ConfigHome: absolute, StateHome: absolute}, true},
		{"empty home", Env{ConfigHome: absolute, StateHome: absolute}, false},
		{"relative config home", Env{Home: absolute, ConfigHome: "c", StateHome: absolute}, false},
		{"empty state home", Env{Home: absolute, ConfigHome: absolute}, false},
	}
	for _, test := range tests {
		if err := test.env.Validate(); (err == nil) != test.valid {
			t.Errorf("%s: Validate() = %v", test.name, err)
		}
	}
}

func TestFromOSReadsTheProcessEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows reads other variables")
	}
	home := t.TempDir()
	config := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_STATE_HOME", "")

	env, err := FromOS()
	if err != nil {
		t.Fatal(err)
	}

	if env.Home != home || env.ConfigHome != config {
		t.Errorf("Home = %q, ConfigHome = %q", env.Home, env.ConfigHome)
	}
	if want := filepath.Join(home, ".local", "state"); env.StateHome != want {
		t.Errorf("StateHome = %q, want %q", env.StateHome, want)
	}
	if env.Getenv == nil || env.LookPath == nil || env.OS == "" {
		t.Errorf("incomplete Env: %+v", env)
	}
}
