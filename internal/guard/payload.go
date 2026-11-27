package guard

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	ClaudeCode  = "claude-code"
	Codex       = "codex"
	GeminiCLI   = "gemini-cli"
	Cursor      = "cursor"
	Antigravity = "antigravity"

	patchBegin  = "*** Begin Patch"
	patchAdd    = "*** Add File: "
	patchUpdate = "*** Update File: "
	patchDelete = "*** Delete File: "
	patchMove   = "*** Move to: "
)

type Edit struct {
	Path             string
	OldText, NewText []byte
	WholeContent     []byte
	Delete           bool
	Opaque           bool
}

type toolCall struct {
	Cwd       string          `json:"cwd"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

type fileInput struct {
	FilePath     string   `json:"file_path"`
	NotebookPath string   `json:"notebook_path"`
	Path         string   `json:"path"`
	Content      *string  `json:"content"`
	OldString    *string  `json:"old_string"`
	NewString    *string  `json:"new_string"`
	Edits        []oldNew `json:"edits"`
}

type oldNew struct {
	OldString *string `json:"old_string"`
	NewString *string `json:"new_string"`
}

func Parse(tool string, payload []byte) ([]Edit, error) {
	switch tool {
	case ClaudeCode:
		return parseClaude(payload)
	case Codex:
		return parseCodex(payload)
	case GeminiCLI:
		return parseGemini(payload)
	case Cursor:
		return parseCursor(payload)
	case Antigravity:
		return parseAntigravity(payload)
	default:
		return nil, fmt.Errorf("unknown tool %q", tool)
	}
}

func decodeCall(payload []byte) (toolCall, fileInput, error) {
	var call toolCall
	if err := json.Unmarshal(payload, &call); err != nil {
		return toolCall{}, fileInput{}, fmt.Errorf("parse payload: %w", err)
	}
	var input fileInput
	if len(call.ToolInput) > 0 && json.Unmarshal(call.ToolInput, &input) != nil {
		return call, fileInput{}, nil
	}
	return call, input, nil
}

func parseClaude(payload []byte) ([]Edit, error) {
	call, input, err := decodeCall(payload)
	if err != nil {
		return nil, err
	}
	switch call.ToolName {
	case "Write":
		return single(call.Cwd, input.FilePath, wholeEdit(input.Content)), nil
	case "Edit":
		return single(call.Cwd, input.FilePath, replaceEdit(input.OldString, input.NewString)), nil
	case "MultiEdit":
		return multiEdit(call.Cwd, input), nil
	case "NotebookEdit":
		return single(call.Cwd, input.NotebookPath, Edit{}), nil
	default:
		return nil, nil
	}
}

func multiEdit(cwd string, input fileInput) []Edit {
	if len(input.Edits) == 0 {
		return single(cwd, input.FilePath, replaceEdit(input.OldString, input.NewString))
	}
	var edits []Edit
	for _, e := range input.Edits {
		edits = append(edits, single(cwd, input.FilePath, replaceEdit(e.OldString, e.NewString))...)
	}
	return edits
}

func parseGemini(payload []byte) ([]Edit, error) {
	call, input, err := decodeCall(payload)
	if err != nil {
		return nil, err
	}
	switch call.ToolName {
	case "write_file":
		return single(call.Cwd, input.FilePath, wholeEdit(input.Content)), nil
	case "replace":
		return single(call.Cwd, input.FilePath, replaceEdit(input.OldString, input.NewString)), nil
	default:
		return nil, nil
	}
}

func parseCursor(payload []byte) ([]Edit, error) {
	var call struct {
		toolCall
		WorkspaceRoots []string `json:"workspace_roots"`
	}
	if err := json.Unmarshal(payload, &call); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	var input fileInput
	if len(call.ToolInput) > 0 && json.Unmarshal(call.ToolInput, &input) != nil {
		return nil, nil
	}
	base := call.Cwd
	if base == "" && len(call.WorkspaceRoots) > 0 {
		base = call.WorkspaceRoots[0]
	}
	return single(base, input.FilePath, Edit{}), nil
}

func parseAntigravity(payload []byte) ([]Edit, error) {
	var call struct {
		Cwd      string `json:"cwd"`
		ToolCall struct {
			Args json.RawMessage `json:"args"`
		} `json:"toolCall"`
	}
	if err := json.Unmarshal(payload, &call); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	var input fileInput
	if len(call.ToolCall.Args) > 0 && json.Unmarshal(call.ToolCall.Args, &input) != nil {
		return nil, nil
	}
	path := input.FilePath
	if path == "" {
		path = input.Path
	}
	return single(call.Cwd, path, Edit{}), nil
}

func parseCodex(payload []byte) ([]Edit, error) {
	var call struct {
		Cwd       string `json:"cwd"`
		ToolInput any    `json:"tool_input"`
	}
	if err := json.Unmarshal(payload, &call); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	var edits []Edit
	for _, patch := range patches(call.ToolInput) {
		edits = append(edits, patchEdits(call.Cwd, patch)...)
	}
	return edits, nil
}

func patches(value any) []string {
	switch v := value.(type) {
	case string:
		if strings.Contains(v, patchBegin) {
			return []string{v}
		}
	case []any:
		var found []string
		for _, entry := range v {
			found = append(found, patches(entry)...)
		}
		return found
	case map[string]any:
		var found []string
		for _, entry := range v {
			found = append(found, patches(entry)...)
		}
		return found
	}
	return nil
}

func patchEdits(cwd, patch string) []Edit {
	var edits []Edit
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimRight(line, "\r")
		for _, op := range []struct {
			prefix string
			edit   Edit
		}{
			{patchAdd, Edit{Opaque: true}},
			{patchUpdate, Edit{Opaque: true}},
			{patchMove, Edit{Opaque: true}},
			{patchDelete, Edit{Delete: true}},
		} {
			if path, ok := strings.CutPrefix(line, op.prefix); ok {
				edits = append(edits, single(cwd, strings.TrimSpace(path), op.edit)...)
			}
		}
	}
	return edits
}

func wholeEdit(content *string) Edit {
	if content == nil {
		return Edit{}
	}
	return Edit{WholeContent: append([]byte{}, *content...)}
}

func replaceEdit(oldString, newString *string) Edit {
	if oldString == nil || newString == nil {
		return Edit{}
	}
	return Edit{OldText: append([]byte{}, *oldString...), NewText: append([]byte{}, *newString...)}
}

func single(cwd, path string, e Edit) []Edit {
	if path == "" {
		return nil
	}
	e.Path = resolve(cwd, path)
	return []Edit{e}
}

func resolve(cwd, path string) string {
	if !filepath.IsAbs(path) {
		if cwd != "" {
			path = filepath.Join(cwd, path)
		} else if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}
	return filepath.Clean(path)
}
