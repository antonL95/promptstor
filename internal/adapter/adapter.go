package adapter

import (
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
)

type ToolID = string

const (
	ClaudeCode  ToolID = "claude-code"
	Codex       ToolID = "codex"
	GeminiCLI   ToolID = "gemini-cli"
	Antigravity ToolID = "antigravity"
	OpenCode    ToolID = "opencode"
	Cursor      ToolID = "cursor"
	AgentsMD    ToolID = "agents-md"
)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

type Target struct {
	Env         paths.Env
	Scope       Scope
	ProjectRoot string
	Selected    []ToolID
}

type Detection struct {
	Installed bool
	Paths     []string
}

type PortableCaps struct{ DenyCommands, DenyPaths, AllowMCPServers bool }

type Capabilities struct {
	Kinds                map[library.Kind]bool
	FileScope            bool
	Activations          map[library.Activation]bool
	Portable             PortableCaps
	ReadsProjectAgentsMD bool
	SkillHomes           map[Scope][]string
	CommandsAsSkills     bool
	EnvRef               func(name string) string
}

type Foreign struct{ Path, Owner, Reason string }

type Grant struct {
	Tool             ToolID
	Path, Key, Value string
}

type Note struct{ Path, Message string }

type Unresolved struct{ File, Line, Target, Reason string }

type Imported struct {
	Items      []library.Item
	Foreign    []Foreign
	Grants     []Grant
	Notes      []Note
	Unresolved []Unresolved
}

type Unsupported struct {
	ItemID   library.ID
	Name     string
	Tool     ToolID
	Reason   string
	Blocking bool
}

type Adapter interface {
	ID() ToolID
	Detect(env paths.Env) Detection
	Capabilities() Capabilities
	Import(t Target) (Imported, error)
	Export(t Target, items []library.Item) ([]plan.Unit, []Unsupported, error)
	BackupSet(t Target) []plan.SnapshotEntry
}
