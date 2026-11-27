package geminicli

import (
	"os"
	"path/filepath"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	tool              = adapter.GeminiCLI
	configDirName     = ".gemini"
	sharedDirName     = ".agents"
	binaryName        = "gemini"
	homeVariable      = "GEMINI_CLI_HOME"
	memoryName        = "GEMINI.md"
	agentsName        = "AGENTS.md"
	settingsName      = "settings.json"
	commandsDir       = "commands"
	agentsDir         = "agents"
	skillsDir         = "skills"
	policiesDir       = "policies"
	extensionsDir     = "extensions"
	skillFile         = "SKILL.md"
	ownPolicy         = "promptstor.toml"
	savedPolicy       = "auto-saved.toml"
	markdownExtension = ".md"
	tomlExtension     = ".toml"
	memoryWriteDetail = "agent memory write"
)

type Adapter struct{}

func init() {
	adapter.Register(Adapter{})
}

func (Adapter) ID() adapter.ToolID { return tool }

func (Adapter) DetectPaths(env paths.Env) []string {
	return []string{filepath.Join(env.Home, configDirName, settingsName)}
}

func (a Adapter) Detect(env paths.Env) adapter.Detection {
	var found adapter.Detection
	for _, path := range a.DetectPaths(env) {
		if exists(path) {
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
		Activations:          map[library.Activation]bool{library.ActivationAlways: true},
		Portable:             adapter.PortableCaps{DenyCommands: true, DenyPaths: true, AllowMCPServers: true},
		ReadsProjectAgentsMD: true,
		SkillHomes: map[adapter.Scope][]string{
			adapter.ScopeUser:    {"~/.agents/skills"},
			adapter.ScopeProject: {".agents/skills"},
		},
		EnvRef: envRef,
	}
}

func (Adapter) HandEditDetail(t adapter.Target, path string, kind manifest.UnitKind) string {
	l, ok := layoutFor(t)
	if !ok || l.scope != adapter.ScopeUser || kind != manifest.UnitSection {
		return ""
	}
	if memory, reason := l.memoryFile(t.Env); reason != "" || path != memory {
		return ""
	}
	return memoryWriteDetail
}

func envRef(name string) string { return "${" + name + "}" }

type layout struct {
	scope            adapter.Scope
	root             string
	config, settings string
}

func layoutFor(t adapter.Target) (layout, bool) {
	var root string
	switch {
	case t.Scope == adapter.ScopeUser:
		root = t.Env.Home
	case t.Scope == adapter.ScopeProject && t.ProjectRoot != "":
		root = t.ProjectRoot
	default:
		return layout{}, false
	}
	config := filepath.Join(root, configDirName)
	return layout{scope: t.Scope, root: root, config: config, settings: filepath.Join(config, settingsName)}, true
}

func (l layout) in(parts ...string) string {
	return filepath.Join(append([]string{l.config}, parts...)...)
}

func (l layout) sharedSkills() string {
	return filepath.Join(l.root, sharedDirName, skillsDir)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
