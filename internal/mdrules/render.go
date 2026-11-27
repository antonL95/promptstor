package mdrules

import (
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/library"
)

func Render(items []library.Item) []byte {
	var bodies []string
	for _, it := range items {
		lines := strings.Split(it.Body, "\n")
		start := slices.IndexFunc(lines, isNotBlank)
		if start < 0 {
			continue
		}
		bodies = append(bodies, joinTrimmed(lines[start:]))
	}
	return []byte(strings.Join(bodies, "\n"))
}

func joinTrimmed(lines []string) string {
	end := len(lines)
	for end > 0 && !isNotBlank(lines[end-1]) {
		end--
	}
	if end == 0 {
		return ""
	}
	return strings.Join(lines[:end], "\n") + "\n"
}

func isNotBlank(line string) bool { return strings.TrimSpace(line) != "" }
