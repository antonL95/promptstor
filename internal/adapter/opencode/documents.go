package opencode

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

	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	noteSymlinked   = "symlinked -> %s; not imported"
	noteCredential  = "matches the credential exclusion list; not imported"
	noteInvalidJSON = "invalid JSON or JSONC; not imported"
)

type document struct {
	path   string
	fields map[string]any
}

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

func readDocuments(env paths.Env, files []string, note func(path, message string)) ([]document, error) {
	var documents []document
	for _, path := range files {
		data, problem, err := readFile(env, path)
		switch {
		case err != nil:
			return nil, err
		case problem != "":
			note(path, problem)
		case data != nil:
			fields, valid := decodeObject(data)
			if !valid {
				note(path, noteInvalidJSON)
				continue
			}
			documents = append(documents, document{path: path, fields: fields})
		}
	}
	return documents, nil
}

func decodeObject(data []byte) (map[string]any, bool) {
	standard, err := hujson.Standardize(slices.Clone(data))
	if err != nil {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(standard))
	decoder.UseNumber()
	var fields map[string]any
	if decoder.Decode(&fields) != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

func object(value any) map[string]any {
	nested, _ := value.(map[string]any)
	return nested
}

func text(value any) string {
	found, _ := value.(string)
	return found
}

func plain(value any) any {
	switch typed := value.(type) {
	case json.Number:
		if whole, err := typed.Int64(); err == nil {
			return int(whole)
		}
		fraction, _ := typed.Float64()
		return fraction
	case map[string]any:
		converted := make(map[string]any, len(typed))
		for key, nested := range typed {
			converted[key] = plain(nested)
		}
		return converted
	case []any:
		converted := make([]any, len(typed))
		for i, nested := range typed {
			converted[i] = plain(nested)
		}
		return converted
	default:
		return value
	}
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
