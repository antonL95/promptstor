package antigravity

import (
	"os"
	"path/filepath"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	tool              = adapter.Antigravity
	geminiDirName     = ".gemini"
	configDirName     = "config"
	workspaceDirName  = ".agents"
	appDirName        = "antigravity"
	ideDirName        = "antigravity-ide"
	cliDirName        = "antigravity-cli"
	rulesDir          = "rules"
	skillsDir         = "skills"
	agentsDir         = "agents"
	pluginsDir        = "plugins"
	hooksName         = "hooks.json"
	mcpName           = "mcp_config.json"
	settingsName      = "settings.json"
	memoryName        = "GEMINI.md"
	agentsName        = "AGENTS.md"
	skillFile         = "SKILL.md"
	agentFile         = "agent.md"
	manifestName      = "import_manifest.json"
	markdownExtension = ".md"
)

var (
	surfaceDirs = []string{appDirName, ideDirName, cliDirName}
	binaryNames = []string{"agy", "antigravity"}
)

type Adapter struct{}

func init() {
	adapter.Register(Adapter{})
}

func (Adapter) ID() adapter.ToolID { return tool }

func (Adapter) DetectPaths(env paths.Env) []string {
	dirs := make([]string, 0, len(surfaceDirs))
	for _, name := range surfaceDirs {
		dirs = append(dirs, filepath.Join(env.Home, geminiDirName, name))
	}
	return dirs
}

func (a Adapter) Detect(env paths.Env) adapter.Detection {
	var found adapter.Detection
	for _, dir := range a.DetectPaths(env) {
		if exists(dir) {
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
			adapter.ScopeUser:    {"~/.gemini/config/skills"},
			adapter.ScopeProject: {".agents/skills"},
		},
		CommandsAsSkills: true,
	}
}

type layout struct {
	scope        adapter.Scope
	root, config string
}

func layoutFor(t adapter.Target) (layout, bool) {
	switch {
	case t.Scope == adapter.ScopeUser:
		return layout{scope: t.Scope, root: t.Env.Home, config: filepath.Join(t.Env.Home, geminiDirName, configDirName)}, true
	case t.Scope == adapter.ScopeProject && t.ProjectRoot != "":
		return layout{scope: t.Scope, root: t.ProjectRoot, config: filepath.Join(t.ProjectRoot, workspaceDirName)}, true
	default:
		return layout{}, false
	}
}

func (l layout) in(parts ...string) string {
	return filepath.Join(append([]string{l.config}, parts...)...)
}

func (l layout) gemini(parts ...string) string {
	return filepath.Join(append([]string{l.root, geminiDirName}, parts...)...)
}

func (l layout) cliSettings() string {
	return l.gemini(cliDirName, settingsName)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
