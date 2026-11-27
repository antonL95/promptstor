package geminicli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tailscale/hujson"

	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	contextKey         = "context"
	fileNameKey        = "fileName"
	contextPointer     = "/context/fileName"
	reasonBadSettings  = "settings.json cannot be parsed, so the memory file is unknown"
	reasonBadMemory    = "context.fileName does not name a Markdown file inside ~/.gemini"
	reasonUserFileName = "context.fileName set by user"
)

var projectContextFiles = []string{agentsName, memoryName}

func readSettings(path string) (doc map[string]any, valid bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	standard, err := hujson.Standardize(data)
	if err != nil || json.Unmarshal(standard, &doc) != nil {
		return nil, false, nil
	}
	return doc, true, nil
}

func object(doc map[string]any, key string) map[string]any {
	nested, _ := doc[key].(map[string]any)
	return nested
}

func textList(value any) []string {
	entries, _ := value.([]any)
	var found []string
	for _, entry := range entries {
		if text, ok := entry.(string); ok {
			found = append(found, text)
		}
	}
	return found
}

func (l layout) memoryFile(env paths.Env) (path, reason string) {
	doc, valid, err := readSettings(l.settings)
	if err != nil || !valid {
		return "", reasonBadSettings
	}
	name, ok := firstContextFile(object(doc, contextKey)[fileNameKey])
	if !ok || name != filepath.Base(name) || !filepath.IsLocal(name) || !strings.EqualFold(filepath.Ext(name), markdownExtension) {
		return "", reasonBadMemory
	}
	path = l.in(name)
	if backup.Excluded(env, path) {
		return "", reasonBadMemory
	}
	return path, ""
}

func firstContextFile(value any) (string, bool) {
	switch typed := value.(type) {
	case nil:
		return memoryName, true
	case string:
		return typed, true
	case []any:
		if len(typed) == 0 {
			return memoryName, true
		}
		name, ok := typed[0].(string)
		return name, ok
	default:
		return "", false
	}
}

func userSetContextFiles(doc map[string]any) bool {
	value, present := object(doc, contextKey)[fileNameKey]
	if !present {
		return false
	}
	entries, ok := value.([]any)
	return !ok || len(entries) != len(projectContextFiles) || !slices.Equal(textList(value), projectContextFiles)
}
