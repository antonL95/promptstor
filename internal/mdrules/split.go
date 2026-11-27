package mdrules

import (
	"path/filepath"
	"strings"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
)

const (
	headingPrefix = "## "
	byteOrderMark = "\xef\xbb\xbf"
	orderStep     = 10
)

type section struct {
	name  string
	lines []string
}

func Split(doc []byte, source string) []library.Item {
	var items []library.Item
	for _, s := range sections(doc, fileName(source)) {
		body := joinTrimmed(s.lines)
		if body == "" {
			continue
		}
		items = append(items, library.Item{
			Kind:   library.KindRule,
			Name:   s.name,
			Order:  (len(items) + 1) * orderStep,
			Body:   body,
			Source: source,
		})
	}
	return items
}

func sections(doc []byte, preamble string) []section {
	text := strings.ReplaceAll(strings.TrimPrefix(string(doc), byteOrderMark), "\r\n", "\n")
	found := []section{{name: preamble}}
	var open fence
	for _, line := range strings.Split(text, "\n") {
		if isManagedLine(line) {
			continue
		}
		if name, ok := heading(line); ok && !open.isOpen() {
			found = append(found, section{name: name})
		}
		open = open.after(line)
		last := &found[len(found)-1]
		last.lines = append(last.lines, line)
	}
	return found
}

func fileName(source string) string {
	base := filepath.Base(source)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func heading(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, headingPrefix)
	name := strings.TrimSpace(rest)
	return name, ok && name != ""
}

func isManagedLine(line string) bool {
	switch strings.TrimRight(line, " \t\r") {
	case managed.Begin, managed.End, managed.Notice:
		return true
	default:
		return false
	}
}
