package library

import (
	"slices"

	"go.yaml.in/yaml/v3"
)

const blockKey = "promptstor"

type block struct {
	ID         ID            `yaml:"id,omitempty"`
	Order      int           `yaml:"order,omitempty"`
	Targets    *blockTargets `yaml:"targets,omitempty"`
	Scope      []string      `yaml:"scope,omitempty"`
	Activation Activation    `yaml:"activation,omitempty"`
	Locked     bool          `yaml:"locked,omitempty"`
	Removed    bool          `yaml:"removed,omitempty"`
}

type blockTargets struct {
	Include []string `yaml:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
}

func decodeItem(kind Kind, slug string, data []byte) (Item, error) {
	l, err := layoutOf(kind)
	if err != nil {
		return Item{}, err
	}
	it := Item{Kind: kind, slug: slug}
	header := data
	if !l.isYAML() {
		var body []byte
		if header, body, err = splitFrontmatter(data); err != nil {
			return Item{}, err
		}
		it.Body = string(body)
	}
	fields, err := parseMapping(header)
	if err != nil {
		return Item{}, err
	}
	switch kind {
	case KindPermission:
		it.Portable = &PortablePermission{}
	case KindMCP:
		it.MCP = &MCPServer{}
	}
	for _, f := range fields {
		if err := it.decodeField(f); err != nil {
			return Item{}, err
		}
	}
	it.nameInFile = it.Name != ""
	if !it.nameInFile {
		it.Name = slug
	}
	return it, nil
}

func (it *Item) decodeField(f field) error {
	switch f.key.Value {
	case "name":
		return decodeValue(f, &it.Name)
	case "description":
		return decodeValue(f, &it.Description)
	case blockKey:
		return it.decodeBlock(f.value)
	}
	if known, err := it.decodeKindField(f); known || err != nil {
		return err
	}
	return it.decodeMeta(f)
}

func (it *Item) decodeKindField(f field) (bool, error) {
	if !slices.Contains(kindKeys(it.Kind), f.key.Value) {
		return false, nil
	}
	var err error
	switch f.key.Value {
	case "deny_commands":
		err = decodeValue(f, &it.Portable.DenyCommands)
	case "deny_paths":
		err = decodeValue(f, &it.Portable.DenyPaths)
	case "allow_mcp_servers":
		err = decodeValue(f, &it.Portable.AllowMCPServers)
	case "native":
		err = decodeValue(f, &it.Native)
	case "transport":
		err = decodeValue(f, &it.MCP.Transport)
	case "command":
		err = decodeValue(f, &it.MCP.Command)
	case "args":
		err = decodeValue(f, &it.MCP.Args)
	case "url":
		err = decodeValue(f, &it.MCP.URL)
	case "env":
		it.MCP.Env, err = decodeValues(f)
	case "headers":
		it.MCP.Headers, err = decodeValues(f)
	case "resolve":
		err = decodeValue(f, &it.MCP.Resolve)
	}
	return true, err
}

func kindKeys(kind Kind) []string {
	switch kind {
	case KindPermission:
		return []string{"deny_commands", "deny_paths", "allow_mcp_servers", "native"}
	case KindHook:
		return []string{"native"}
	case KindMCP:
		return []string{"transport", "command", "args", "url", "env", "headers", "resolve"}
	default:
		return nil
	}
}

func (it *Item) decodeBlock(node *yaml.Node) error {
	var decoded block
	var targets yaml.Node
	err := decodeKnown(node, blockKey, map[string]any{
		"id":         &decoded.ID,
		"order":      &decoded.Order,
		"targets":    &targets,
		"scope":      &decoded.Scope,
		"activation": &decoded.Activation,
		"locked":     &decoded.Locked,
		"removed":    &decoded.Removed,
	})
	if err != nil {
		return err
	}
	if targets.Kind != 0 {
		err = decodeKnown(&targets, "targets", map[string]any{
			"include": &it.Targets.Include,
			"exclude": &it.Targets.Exclude,
		})
		if err != nil {
			return err
		}
	}
	it.ID, it.Order, it.Scope = decoded.ID, decoded.Order, decoded.Scope
	it.Activation, it.Locked, it.Removed = decoded.Activation, decoded.Locked, decoded.Removed
	return validateActivation(it.Activation)
}

func (it *Item) decodeMeta(f field) error {
	var value any
	if err := decodeValue(f, &value); err != nil {
		return err
	}
	if it.Meta == nil {
		it.Meta = map[string]any{}
	}
	it.Meta[f.key.Value] = value
	it.metaOrder = append(it.metaOrder, metaNode(f))
	return nil
}
