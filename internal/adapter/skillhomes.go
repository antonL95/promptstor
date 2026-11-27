package adapter

import (
	"bytes"
	"cmp"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	skillFile      = "SKILL.md"
	skillFileMode  = fs.FileMode(0o644)
	userAgents     = "~/.agents/skills"
	userClaude     = "~/.claude/skills"
	userGemini     = "~/.gemini/config/skills"
	projectAgents  = ".agents/skills"
	projectClaude  = ".claude/skills"
	frontmatterTag = "---\n"
)

func skillHome(scope Scope, tool ToolID) (string, bool) {
	switch {
	case scope == ScopeUser && tool == ClaudeCode:
		return userClaude, true
	case scope == ScopeUser && tool == Antigravity:
		return userGemini, true
	case scope == ScopeUser && slices.Contains([]ToolID{Codex, GeminiCLI, OpenCode, Cursor}, tool):
		return userAgents, true
	case scope == ScopeProject && tool == ClaudeCode:
		return projectClaude, true
	case scope == ScopeProject && slices.Contains([]ToolID{Codex, GeminiCLI, OpenCode, Cursor, Antigravity}, tool):
		return projectAgents, true
	default:
		return "", false
	}
}

func SkillReaders(scope Scope, dir string) []ToolID {
	var readers []ToolID
	switch {
	case scope == ScopeUser && dir == userAgents:
		readers = []ToolID{Codex, GeminiCLI, OpenCode, Cursor}
	case scope == ScopeUser && dir == userClaude:
		readers = []ToolID{ClaudeCode, OpenCode, Cursor}
	case scope == ScopeUser && dir == userGemini:
		readers = []ToolID{Antigravity}
	case scope == ScopeProject && dir == projectAgents:
		readers = []ToolID{Codex, GeminiCLI, OpenCode, Cursor, Antigravity}
	case scope == ScopeProject && dir == projectClaude:
		readers = []ToolID{ClaudeCode, OpenCode, Cursor}
	}
	slices.Sort(readers)
	return readers
}

func SkillUnits(t Target, tool ToolID, it library.Item) []plan.Unit {
	home, ok := skillHome(t.Scope, tool)
	if !ok || !isFolderName(it.Name) {
		return nil
	}
	root, dir := t.Env.Home, t.Env.Expand(home)
	if t.Scope == ScopeProject {
		root, dir = t.ProjectRoot, filepath.Join(t.ProjectRoot, filepath.FromSlash(home))
	}
	folder := filepath.Join(dir, it.Name)
	document, err := renderSkill(it)
	if err != nil {
		return nil
	}
	newUnit := func(path string, content []byte, mode fs.FileMode) plan.Unit {
		return plan.Unit{
			Tool:    tool,
			Path:    path,
			Root:    root,
			Kind:    manifest.UnitFile,
			Content: content,
			Mode:    mode,
			Items:   []library.ID{it.ID},
		}
	}
	units := []plan.Unit{newUnit(filepath.Join(folder, skillFile), document, skillFileMode)}
	for _, key := range slices.Sorted(maps.Keys(it.Files)) {
		relative := filepath.FromSlash(key)
		if !filepath.IsLocal(relative) || relative == skillFile {
			return nil
		}
		file := it.Files[key]
		units = append(units, newUnit(filepath.Join(folder, relative), slices.Clone(file.Data), attachmentMode(file)))
	}
	slices.SortFunc(units, func(a, b plan.Unit) int { return cmp.Compare(a.Path, b.Path) })
	return units
}

func isFolderName(name string) bool {
	return name != "" && filepath.IsLocal(name) && filepath.Base(name) == name
}

func attachmentMode(file library.File) fs.FileMode {
	if file.Mode.Perm() == 0 {
		return skillFileMode
	}
	return file.Mode.Perm()
}

func renderSkill(it library.Item) ([]byte, error) {
	header := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	header.Content = append(header.Content, stringNode("name"), stringNode(it.Name))
	if it.Description != "" {
		header.Content = append(header.Content, stringNode("description"), stringNode(it.Description))
	}
	for _, key := range it.MetaKeys() {
		if key == "name" || key == "description" {
			continue
		}
		value := &yaml.Node{}
		if err := value.Encode(it.Meta[key]); err != nil {
			return nil, err
		}
		header.Content = append(header.Content, stringNode(key), value)
	}
	var buffer bytes.Buffer
	buffer.WriteString(frontmatterTag)
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(header); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	buffer.WriteString(frontmatterTag)
	buffer.WriteString(it.Body)
	return managed.WithNotice(buffer.Bytes()), nil
}

func stringNode(value string) *yaml.Node {
	node := &yaml.Node{}
	node.SetString(value)
	return node
}
