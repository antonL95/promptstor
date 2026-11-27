package structedit

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
)

type statementKind int

const (
	blankStatement statementKind = iota
	commentStatement
	headerStatement
	keyStatement
)

type statement struct {
	kind       statementKind
	start, end int
	path       []string
	array      bool
}

func (s statement) isTableHeader(path []string) bool {
	return s.kind == headerStatement && !s.array && slices.Equal(s.path, path)
}

func scanTOML(doc []byte) ([]statement, error) {
	var statements []statement
	for at := 0; at < len(doc); {
		next, err := scanStatement(doc, at)
		if err != nil {
			return nil, fmt.Errorf("read statement at byte %d: %w", at, err)
		}
		statements = append(statements, next)
		at = next.end
	}
	return statements, nil
}

func scanStatement(doc []byte, start int) (statement, error) {
	at := skipBlanks(doc, start)
	found := statement{start: start, end: lineEnd(doc, at)}
	switch {
	case at == len(doc) || doc[at] == '\n' || doc[at] == '\r':
		found.kind = blankStatement
	case doc[at] == '#':
		found.kind = commentStatement
	case doc[at] == '[':
		found.kind = headerStatement
		at++
		if at < len(doc) && doc[at] == '[' {
			found.array = true
			at++
		}
		path, _, err := parseDottedKey(doc, at)
		found.path = path
		return found, err
	default:
		found.kind = keyStatement
		path, after, err := parseDottedKey(doc, at)
		if err != nil {
			return found, err
		}
		if after == len(doc) || doc[after] != '=' {
			return found, errors.New("key has no value")
		}
		found.path = path
		found.end = valueEnd(doc, after+1)
	}
	return found, nil
}

func skipBlanks(doc []byte, at int) int {
	for at < len(doc) && (doc[at] == ' ' || doc[at] == '\t') {
		at++
	}
	return at
}

func lineEnd(doc []byte, at int) int {
	if next := bytes.IndexByte(doc[at:], '\n'); next >= 0 {
		return at + next + 1
	}
	return len(doc)
}

func valueEnd(doc []byte, at int) int {
	depth := 0
	for at < len(doc) {
		switch doc[at] {
		case '"', '\'':
			at = stringEnd(doc, at)
			continue
		case '#':
			if depth == 0 {
				return lineEnd(doc, at)
			}
			at = lineEnd(doc, at)
			continue
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case '\n':
			if depth == 0 {
				return at + 1
			}
		}
		at++
	}
	return len(doc)
}

func stringEnd(doc []byte, at int) int {
	quote := doc[at]
	delimiter := []byte{quote}
	if triple := []byte{quote, quote, quote}; bytes.HasPrefix(doc[at:], triple) {
		delimiter = triple
	}
	at += len(delimiter)
	for at < len(doc) {
		if quote == '"' && doc[at] == '\\' {
			at += 2
			continue
		}
		if bytes.HasPrefix(doc[at:], delimiter) {
			at += len(delimiter)
			for len(delimiter) == 3 && at < len(doc) && doc[at] == quote {
				at++
			}
			return at
		}
		at++
	}
	return len(doc)
}

func parseTablePath(table string) ([]string, error) {
	path, after, err := parseDottedKey([]byte(table), 0)
	if err == nil && after != len(table) {
		err = errors.New("unexpected text after the table path")
	}
	if err != nil {
		return nil, fmt.Errorf("table path: %w", err)
	}
	return path, nil
}

func parseDottedKey(doc []byte, at int) ([]string, int, error) {
	var path []string
	for {
		segment, after, err := parseKeySegment(doc, skipBlanks(doc, at))
		if err != nil {
			return nil, 0, err
		}
		path = append(path, segment)
		at = skipBlanks(doc, after)
		if at == len(doc) || doc[at] != '.' {
			return path, at, nil
		}
		at++
	}
}

func parseKeySegment(doc []byte, at int) (string, int, error) {
	if at < len(doc) && (doc[at] == '"' || doc[at] == '\'') {
		end := stringEnd(doc, at)
		var decoded map[string]any
		if err := decodeTOML(slices.Concat([]byte("key = "), doc[at:end]), &decoded); err != nil {
			return "", 0, fmt.Errorf("quoted key: %w", err)
		}
		segment, ok := decoded["key"].(string)
		if !ok {
			return "", 0, errors.New("quoted key is not a string")
		}
		return segment, end, nil
	}
	end := at
	for end < len(doc) && isBareKeyByte(doc[end]) {
		end++
	}
	if end == at {
		return "", 0, errors.New("missing key")
	}
	return string(doc[at:end]), end, nil
}

func isBareKeyByte(b byte) bool {
	return b == '-' || b == '_' || ('0' <= b && b <= '9') || ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z')
}
