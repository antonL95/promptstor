package codex

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func decodeTable(data []byte) (map[string]any, bool) {
	var table map[string]any
	if err := toml.Unmarshal(data, &table); err != nil {
		return nil, false
	}
	return table, true
}

func subtable(table map[string]any, key string) map[string]any {
	nested, _ := table[key].(map[string]any)
	return nested
}

func readConfig(path string) (map[string]any, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	table, valid := decodeTable(data)
	return table, valid, nil
}

func tomlKey(key string) string {
	if bareKey.MatchString(key) {
		return key
	}
	return tomlString(key)
}

func tomlString(value string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteRune('\\')
			out.WriteRune(r)
		case '\n':
			out.WriteString(`\n`)
		case '\t':
			out.WriteString(`\t`)
		default:
			writeTOMLRune(&out, r)
		}
	}
	out.WriteByte('"')
	return out.String()
}

func tomlMultiline(value string) string {
	var out strings.Builder
	out.WriteString("\"\"\"\n")
	quotes := 0
	for i, r := range value {
		if r != '"' {
			quotes = 0
		}
		switch r {
		case '"':
			if quotes++; quotes == 3 || i == len(value)-1 {
				out.WriteRune('\\')
				quotes = 0
			}
			out.WriteRune(r)
		case '\\':
			out.WriteString(`\\`)
		case '\n', '\t':
			out.WriteRune(r)
		default:
			writeTOMLRune(&out, r)
		}
	}
	out.WriteString(`"""`)
	return out.String()
}

func writeTOMLRune(out *strings.Builder, r rune) {
	if r < 0x20 || r == 0x7f {
		fmt.Fprintf(out, `\u%04X`, r)
		return
	}
	out.WriteRune(r)
}

func tomlArray(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = tomlString(value)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func tomlInlineTable(values map[string]string) string {
	var pairs []string
	for _, key := range slices.Sorted(maps.Keys(values)) {
		pairs = append(pairs, tomlString(key)+" = "+tomlString(values[key]))
	}
	return "{ " + strings.Join(pairs, ", ") + " }"
}
