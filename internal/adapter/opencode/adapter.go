package opencode

import (
	"os"
	"path/filepath"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	tool               = adapter.OpenCode
	appName            = "opencode"
	binaryName         = "opencode"
	configDirVariable  = "OPENCODE_CONFIG_DIR"
	projectDirName     = ".opencode"
	sharedDirName      = ".agents"
	jsonName           = "opencode.json"
	jsoncName          = "opencode.jsonc"
	instructionsName   = "AGENTS.md"
	skillFile          = "SKILL.md"
	skillsDir          = "skills"
	legacySkillsDir    = "skill"
	agentsDir          = "agents"
	legacyAgentsDir    = "agent"
	modesDir           = "modes"
	legacyModesDir     = "mode"
	commandsDir        = "commands"
	legacyCommandsDir  = "command"
	markdownExtension  = ".md"
	commandSeparator   = ":"
	agentSeparator     = "/"
	reasonUnknownLocal = "OPENCODE_CONFIG_DIR is not an absolute path, so the opencode config folder is unknown"
)

var configNames = []string{jsonName, jsoncName}

type Adapter struct{}

func init() {
	adapter.Register(Adapter{})
}

func (Adapter) ID() adapter.ToolID { return tool }

func (Adapter) Detect(env paths.Env) adapter.Detection {
	var found adapter.Detection
	if dir, known := userConfigDir(env); known {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			found.Paths = []string{dir}
		}
	}
	found.Installed = len(found.Paths) > 0
	if env.LookPath != nil {
		if _, err := env.LookPath(binaryName); err == nil {
			found.Installed = true
		}
	}
	return found
}

func (Adapter) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{
		Kinds: map[library.Kind]bool{
			library.KindRule:       true,
			library.KindSkill:      true,
			library.KindCommand:    true,
			library.KindSubagent:   true,
			library.KindPermission: true,
			library.KindMCP:        true,
		},
		Activations:          map[library.Activation]bool{library.ActivationAlways: true},
		Portable:             adapter.PortableCaps{DenyCommands: true, DenyPaths: true},
		ReadsProjectAgentsMD: true,
		SkillHomes: map[adapter.Scope][]string{
			adapter.ScopeUser:    {"~/.agents/skills"},
			adapter.ScopeProject: {".agents/skills"},
		},
		EnvRef: envRef,
	}
}

func envRef(name string) string { return "{env:" + name + "}" }

func userConfigDir(env paths.Env) (string, bool) {
	if override := variable(env, configDirVariable); override != "" {
		return filepath.Clean(override), filepath.IsAbs(override)
	}
	return filepath.Join(env.ConfigHome, appName), true
}

func variable(env paths.Env, name string) string {
	if env.Getenv == nil {
		return ""
	}
	return env.Getenv(name)
}

type layout struct {
	scope         adapter.Scope
	root          string
	base, content string
	sharedRoot    string
	unknownConfig bool
}

func layoutFor(t adapter.Target) (layout, bool) {
	switch {
	case t.Scope == adapter.ScopeUser:
		l := layout{scope: t.Scope, root: t.Env.Home, sharedRoot: t.Env.Home}
		dir, known := userConfigDir(t.Env)
		if !known {
			l.unknownConfig = true
			return l, true
		}
		if !within(t.Env.Home, dir) {
			l.root = filepath.Dir(dir)
		}
		l.base, l.content = dir, dir
		return l, true
	case t.Scope == adapter.ScopeProject && t.ProjectRoot != "":
		return layout{
			scope:      t.Scope,
			root:       t.ProjectRoot,
			base:       t.ProjectRoot,
			content:    filepath.Join(t.ProjectRoot, projectDirName),
			sharedRoot: t.ProjectRoot,
		}, true
	default:
		return layout{}, false
	}
}

func (l layout) in(parts ...string) string {
	return filepath.Join(append([]string{l.content}, parts...)...)
}

func (l layout) at(name string) string { return filepath.Join(l.base, name) }

func (l layout) sharedSkills() string {
	return filepath.Join(l.sharedRoot, sharedDirName, skillsDir)
}

func (l layout) configPaths() []string {
	if l.unknownConfig {
		return nil
	}
	dirs := []string{l.base}
	if l.scope == adapter.ScopeProject {
		dirs = append(dirs, l.content)
	}
	var found []string
	for _, dir := range dirs {
		for _, name := range configNames {
			found = append(found, filepath.Join(dir, name))
		}
	}
	return found
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && filepath.IsLocal(relative)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
