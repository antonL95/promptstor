package cursor

import (
	"os"
	"path/filepath"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	tool              = adapter.Cursor
	configDirName     = ".cursor"
	sharedDirName     = ".agents"
	skillsDir         = "skills"
	rulesDir          = "rules"
	commandsDir       = "commands"
	agentsDir         = "agents"
	mcpName           = "mcp.json"
	hooksName         = "hooks.json"
	userCLIName       = "cli-config.json"
	projectCLIName    = "cli.json"
	editorPermissions = "permissions.json"
	legacyRulesName   = ".cursorrules"
	skillFile         = "SKILL.md"
	ruleExtension     = ".mdc"
	markdownExtension = ".md"
)

var binaryNames = []string{"cursor", "cursor-agent"}

type Adapter struct{}

func init() {
	adapter.Register(Adapter{})
}

func (Adapter) ID() adapter.ToolID { return tool }

func (Adapter) DetectPaths(env paths.Env) []string {
	return []string{filepath.Join(env.Home, configDirName)}
}

func (a Adapter) Detect(env paths.Env) adapter.Detection {
	var found adapter.Detection
	for _, dir := range a.DetectPaths(env) {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			found.Paths = append(found.Paths, dir)
		}
	}
	found.Installed = len(found.Paths) > 0
	if env.LookPath != nil {
		for _, name := range binaryNames {
			if _, err := env.LookPath(name); err == nil {
				found.Installed = true
			}
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
			library.ActivationAgent:  true,
		},
		Portable:             adapter.PortableCaps{DenyCommands: true, DenyPaths: true},
		ReadsProjectAgentsMD: true,
		SkillHomes: map[adapter.Scope][]string{
			adapter.ScopeUser:    {"~/.agents/skills"},
			adapter.ScopeProject: {".agents/skills"},
		},
		EnvRef: envRef,
	}
}

func envRef(name string) string { return "${env:" + name + "}" }

type layout struct {
	scope        adapter.Scope
	root, config string
}

func layoutFor(t adapter.Target) (layout, bool) {
	switch {
	case t.Scope == adapter.ScopeUser:
		return layout{scope: t.Scope, root: t.Env.Home, config: filepath.Join(t.Env.Home, configDirName)}, true
	case t.Scope == adapter.ScopeProject && t.ProjectRoot != "":
		return layout{scope: t.Scope, root: t.ProjectRoot, config: filepath.Join(t.ProjectRoot, configDirName)}, true
	default:
		return layout{}, false
	}
}

func (l layout) in(parts ...string) string {
	return filepath.Join(append([]string{l.config}, parts...)...)
}

func (l layout) sharedSkills() string {
	return filepath.Join(l.root, sharedDirName, skillsDir)
}

func (l layout) cliConfig() string {
	if l.scope == adapter.ScopeProject {
		return l.in(projectCLIName)
	}
	return l.in(userCLIName)
}

func cliConfigMoved(t adapter.Target) bool {
	if t.Scope != adapter.ScopeUser {
		return false
	}
	env := t.Env
	if env.Getenv != nil && env.Getenv("CURSOR_CONFIG_DIR") != "" {
		return true
	}
	if env.OS != "linux" || env.ConfigHome == "" {
		return false
	}
	return env.ConfigHome != filepath.Join(env.Home, ".config") || exists(filepath.Join(env.ConfigHome, "cursor", userCLIName))
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
