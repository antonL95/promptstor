package claudecode

import (
	"os"
	"path/filepath"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	tool              = adapter.ClaudeCode
	configDirName     = ".claude"
	instructionsName  = "CLAUDE.md"
	agentsName        = "AGENTS.md"
	stateName         = ".claude.json"
	projectMCPName    = ".mcp.json"
	settingsName      = "settings.json"
	localSettingsName = "settings.local.json"
	boostConfigName   = "boost.json"
	configDirVariable = "CLAUDE_CONFIG_DIR"
	binaryName        = "claude"
	rulesDir          = "rules"
	commandsDir       = "commands"
	agentsDir         = "agents"
	skillsDir         = "skills"
	pluginsDir        = "plugins"
)

type Adapter struct{}

func init() {
	adapter.Register(Adapter{})
}

func (Adapter) ID() adapter.ToolID { return tool }

func (Adapter) DetectPaths(env paths.Env) []string {
	return []string{filepath.Join(env.Home, configDirName), filepath.Join(env.Home, stateName)}
}

func (a Adapter) Detect(env paths.Env) adapter.Detection {
	var found adapter.Detection
	for _, path := range a.DetectPaths(env) {
		if _, err := os.Lstat(path); err == nil {
			found.Paths = append(found.Paths, path)
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
			library.KindHook:       true,
			library.KindMCP:        true,
		},
		FileScope: true,
		Activations: map[library.Activation]bool{
			library.ActivationAlways: true,
			library.ActivationFiles:  true,
		},
		Portable:             adapter.PortableCaps{DenyCommands: true, DenyPaths: true, AllowMCPServers: true},
		ReadsProjectAgentsMD: true,
		SkillHomes: map[adapter.Scope][]string{
			adapter.ScopeUser:    {"~/.claude/skills"},
			adapter.ScopeProject: {".claude/skills"},
		},
		EnvRef: envRef,
	}
}

func envRef(name string) string { return "${" + name + "}" }

func SharedStateFiles(env paths.Env) []string {
	return []string{filepath.Join(env.Home, stateName)}
}

type layout struct {
	scope        adapter.Scope
	root, config string
	instructions string
	settings     string
	mcp          string
}

func layoutFor(t adapter.Target) (layout, bool) {
	switch {
	case t.Scope == adapter.ScopeUser:
		config := filepath.Join(t.Env.Home, configDirName)
		return layout{
			scope:        t.Scope,
			root:         t.Env.Home,
			config:       config,
			instructions: filepath.Join(config, instructionsName),
			settings:     filepath.Join(config, settingsName),
			mcp:          filepath.Join(t.Env.Home, stateName),
		}, true
	case t.Scope == adapter.ScopeProject && t.ProjectRoot != "":
		config := filepath.Join(t.ProjectRoot, configDirName)
		return layout{
			scope:        t.Scope,
			root:         t.ProjectRoot,
			config:       config,
			instructions: filepath.Join(t.ProjectRoot, instructionsName),
			settings:     filepath.Join(config, settingsName),
			mcp:          filepath.Join(t.ProjectRoot, projectMCPName),
		}, true
	default:
		return layout{}, false
	}
}

func (l layout) in(parts ...string) string {
	return filepath.Join(append([]string{l.config}, parts...)...)
}
