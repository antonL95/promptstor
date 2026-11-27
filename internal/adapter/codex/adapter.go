package codex

import (
	"os"
	"path/filepath"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	tool             = adapter.Codex
	configDirName    = ".codex"
	homeVariable     = "CODEX_HOME"
	binaryName       = "codex"
	instructionsName = "AGENTS.md"
	overrideName     = "AGENTS.override.md"
	configName       = "config.toml"
	profileSuffix    = ".config.toml"
	hooksName        = "hooks.json"
	rulesDir         = "rules"
	agentsDir        = "agents"
	skillsDir        = "skills"
	promptsDir       = "prompts"
	pluginsDir       = "plugins"
	systemDir        = ".system"
	sharedDir        = ".agents"
	defaultRules     = "default.rules"
	ownRules         = "promptstor.rules"
	skillFile        = "SKILL.md"
)

type Adapter struct{}

func init() {
	adapter.Register(Adapter{})
}

func (Adapter) ID() adapter.ToolID { return tool }

func (Adapter) Detect(env paths.Env) adapter.Detection {
	var found adapter.Detection
	if home := codexHome(env); exists(home) {
		found.Paths = []string{home}
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
			library.KindHook:       true,
			library.KindMCP:        true,
		},
		Activations:          map[library.Activation]bool{library.ActivationAlways: true},
		Portable:             adapter.PortableCaps{DenyCommands: true, DenyPaths: true},
		ReadsProjectAgentsMD: true,
		SkillHomes: map[adapter.Scope][]string{
			adapter.ScopeUser:    {"~/.agents/skills"},
			adapter.ScopeProject: {".agents/skills"},
		},
		CommandsAsSkills: true,
		EnvRef:           envRef,
	}
}

func envRef(name string) string { return "${env:" + name + "}" }

func codexHome(env paths.Env) string {
	if env.Getenv != nil {
		if dir := env.Getenv(homeVariable); filepath.IsAbs(dir) {
			return filepath.Clean(dir)
		}
	}
	return filepath.Join(env.Home, configDirName)
}

type layout struct {
	scope        adapter.Scope
	root, config string
	instructions string
	sharedRoot   string
}

func layoutFor(t adapter.Target) (layout, bool) {
	switch {
	case t.Scope == adapter.ScopeUser:
		config := codexHome(t.Env)
		return layout{
			scope:        t.Scope,
			root:         rootOf(t.Env.Home, config),
			config:       config,
			instructions: filepath.Join(config, instructionsName),
			sharedRoot:   t.Env.Home,
		}, true
	case t.Scope == adapter.ScopeProject && t.ProjectRoot != "":
		return layout{
			scope:        t.Scope,
			root:         t.ProjectRoot,
			config:       filepath.Join(t.ProjectRoot, configDirName),
			instructions: filepath.Join(t.ProjectRoot, instructionsName),
			sharedRoot:   t.ProjectRoot,
		}, true
	default:
		return layout{}, false
	}
}

func rootOf(home, dir string) string {
	if relative, err := filepath.Rel(home, dir); err == nil && relative != "." && filepath.IsLocal(relative) {
		return home
	}
	return filepath.Dir(dir)
}

func (l layout) in(parts ...string) string {
	return filepath.Join(append([]string{l.config}, parts...)...)
}

func (l layout) sharedSkills() string {
	return filepath.Join(l.sharedRoot, sharedDir, skillsDir)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
