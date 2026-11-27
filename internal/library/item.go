package library

import (
	"io/fs"
	"maps"
	"slices"

	"go.yaml.in/yaml/v3"
)

type Kind string

const (
	KindRule       Kind = "rule"
	KindSkill      Kind = "skill"
	KindCommand    Kind = "command"
	KindSubagent   Kind = "subagent"
	KindPermission Kind = "permission"
	KindHook       Kind = "hook"
	KindMCP        Kind = "mcp"
)

type Layer string

const (
	LayerCompany  Layer = "company"
	LayerTeam     Layer = "team"
	LayerProject  Layer = "project"
	LayerPersonal Layer = "personal"
	LayerDevice   Layer = "device"
)

type Activation string

const (
	ActivationAlways Activation = "always"
	ActivationFiles  Activation = "files"
	ActivationAgent  Activation = "agent"
)

type ID string

type Targets struct{ Include, Exclude []string }

type PortablePermission struct{ DenyCommands, DenyPaths, AllowMCPServers []string }

type Value struct{ Literal, Ref string }

type MCPServer struct {
	Transport, Command, URL string
	Args                    []string
	Env, Headers            map[string]Value
	Resolve                 bool
}

type Item struct {
	ID              ID
	Kind            Kind
	Name            string
	Layer           Layer
	Order           int
	Description     string
	Targets         Targets
	Scope           []string
	Activation      Activation
	Locked, Removed bool
	Body            string
	Meta            map[string]any
	Files           map[string]File
	Native          map[string]any
	Portable        *PortablePermission
	MCP             *MCPServer
	Source          string

	slug       string
	nameInFile bool
	metaOrder  []metaNode
}

type File struct {
	Data []byte
	Mode fs.FileMode
}

type metaNode struct{ key, value *yaml.Node }

func (it Item) MetaKeys() []string {
	keys := make([]string, 0, len(it.Meta))
	seen := map[string]bool{}
	for _, original := range it.metaOrder {
		key := original.key.Value
		if _, present := it.Meta[key]; present && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(it.Meta)) {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	return keys
}
