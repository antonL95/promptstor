package cursor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tailscale/hujson"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	noteSymlinked  = "symlinked -> %s; not imported"
	noteCredential = "matches the credential exclusion list; not imported"
)

func readFile(env paths.Env, path string) (data []byte, problem string, err error) {
	resolved, err := filepath.EvalSymlinks(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if exists(path) {
			target, _ := os.Readlink(path)
			return nil, fmt.Sprintf(noteSymlinked, target), nil
		}
		return nil, "", nil
	case err != nil:
		return nil, "", fmt.Errorf("read %s: %w", path, err)
	case backup.Excluded(env, path) || backup.Excluded(env, resolved):
		return nil, noteCredential, nil
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", path, err)
	}
	if data == nil {
		data = []byte{}
	}
	return data, "", nil
}

func readExisting(t adapter.Target, path string) (map[string]json.RawMessage, error) {
	data, _, err := readFile(t.Env, path)
	if err != nil || data == nil {
		return nil, err
	}
	fields, _ := decodeObject(data)
	return fields, nil
}

func decodeObject(data []byte) (map[string]json.RawMessage, bool) {
	standard, err := hujson.Standardize(slices.Clone(data))
	var fields map[string]json.RawMessage
	if err != nil || json.Unmarshal(standard, &fields) != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

func rawObject(raw json.RawMessage) map[string]json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	return fields
}

func textList(raw json.RawMessage) []string {
	var entries []any
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	var found []string
	for _, entry := range entries {
		if text, ok := entry.(string); ok {
			found = append(found, text)
		}
	}
	return found
}

func marshal(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func escapePointer(token string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(token)
}
