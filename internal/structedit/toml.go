package structedit

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"github.com/pelletier/go-toml/v2"
)

var ErrUnsupportedLayout = errors.New("TOML layout cannot be edited without touching other content")

func TOMLGetTable(doc []byte, table string) ([]byte, bool, error) {
	block, found, err := locateTable(doc, table)
	if err != nil {
		return nil, false, fmt.Errorf("get TOML table %q: %w", table, err)
	}
	if !found {
		return nil, false, nil
	}
	return slices.Clone(doc[block.start:block.end]), true, nil
}

func TOMLSetTable(doc []byte, table string, block []byte) ([]byte, error) {
	out, err := tomlSetTable(doc, table, block)
	if err != nil {
		return nil, fmt.Errorf("set TOML table %q: %w", table, err)
	}
	return out, nil
}

func TOMLDeleteTable(doc []byte, table string) ([]byte, error) {
	out, err := tomlDeleteTable(doc, table)
	if err != nil {
		return nil, fmt.Errorf("delete TOML table %q: %w", table, err)
	}
	return out, nil
}

type span struct{ start, end int }

func locateTable(doc []byte, table string) (span, bool, error) {
	path, err := parseTablePath(table)
	if err != nil {
		return span{}, false, err
	}
	var decoded map[string]any
	if err := decodeTOML(doc, &decoded); err != nil {
		return span{}, false, fmt.Errorf("document: %w", err)
	}
	statements, err := scanTOML(doc)
	if err != nil {
		return span{}, false, err
	}
	finder := tableFinder{path: path}
	for _, statement := range statements {
		if err := finder.visit(statement); err != nil {
			return span{}, false, err
		}
	}
	if !finder.started && definedInValues(decoded, path) {
		return span{}, false, ErrUnsupportedLayout
	}
	return finder.block, finder.started, nil
}

type tableFinder struct {
	path    []string
	section []string
	block   span
	started bool
	closed  bool
}

func (f *tableFinder) visit(s statement) error {
	switch s.kind {
	case headerStatement:
		return f.visitHeader(s)
	case keyStatement:
		return f.visitKey(s)
	default:
		return nil
	}
}

func (f *tableFinder) visitHeader(s statement) error {
	f.section = s.path
	if s.array && hasPathPrefix(f.path, s.path) {
		return ErrUnsupportedLayout
	}
	if !hasPathPrefix(s.path, f.path) {
		f.closed = f.started
		return nil
	}
	if f.closed || (!f.started && !slices.Equal(s.path, f.path)) {
		return ErrUnsupportedLayout
	}
	if !f.started {
		f.started = true
		f.block.start = s.start
	}
	f.block.end = s.end
	return nil
}

func (f *tableFinder) visitKey(s statement) error {
	full := slices.Concat(f.section, s.path)
	if hasPathPrefix(f.path, full) {
		return ErrUnsupportedLayout
	}
	for i := len(f.section) + 1; i < len(full); i++ {
		if hasPathPrefix(f.path, full[:i]) {
			return ErrUnsupportedLayout
		}
	}
	inBlock := f.started && !f.closed
	if hasPathPrefix(full, f.path) && !inBlock {
		return ErrUnsupportedLayout
	}
	if inBlock {
		f.block.end = s.end
	}
	return nil
}

func hasPathPrefix(path, prefix []string) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

func definedInValues(table map[string]any, path []string) bool {
	value, ok := table[path[0]]
	if !ok {
		return false
	}
	nested, isTable := value.(map[string]any)
	if !isTable || len(path) == 1 {
		return true
	}
	return definedInValues(nested, path[1:])
}

func decodeTOML(content []byte, into *map[string]any) error {
	err := toml.Unmarshal(content, into)
	if err == nil {
		return nil
	}
	var decodeError *toml.DecodeError
	if errors.As(err, &decodeError) {
		row, column := decodeError.Position()
		return fmt.Errorf("invalid TOML at line %d, column %d", row, column)
	}
	return errors.New("invalid TOML")
}

func tomlSetTable(doc []byte, table string, block []byte) ([]byte, error) {
	body, err := tableBody(table, block)
	if err != nil {
		return nil, err
	}
	existing, found, err := locateTable(doc, table)
	if err != nil {
		return nil, err
	}
	newline := []byte(newlineOf(doc))
	terminated := len(doc) == 0 || bytes.HasSuffix(doc, []byte("\n"))
	var out []byte
	switch {
	case found:
		if bytes.HasSuffix(doc[:existing.end], []byte("\n")) {
			body = slices.Concat(body, newline)
		}
		out = slices.Concat(doc[:existing.start], body, doc[existing.end:])
	case len(doc) == 0:
		out = slices.Concat(body, newline)
	case terminated:
		out = slices.Concat(doc, newline, body, newline)
	default:
		out = slices.Concat(doc, newline, newline, body)
	}
	return verifiedTOML(out)
}

func tableBody(table string, block []byte) ([]byte, error) {
	path, err := parseTablePath(table)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if err := decodeTOML(block, &decoded); err != nil {
		return nil, fmt.Errorf("block: %w", err)
	}
	statements, err := scanTOML(block)
	if err != nil {
		return nil, fmt.Errorf("block: %w", err)
	}
	if len(statements) == 0 || !statements[0].isTableHeader(path) {
		return nil, errors.New("block must start with the header of the table")
	}
	var last statement
	for i, s := range statements {
		if s.kind == headerStatement && i > 0 && (!hasPathPrefix(s.path, path) || len(s.path) == len(path)) {
			return nil, errors.New("block defines something outside the table")
		}
		if s.kind != blankStatement {
			last = s
		}
	}
	if last.kind == commentStatement {
		return nil, errors.New("block must not end with a comment")
	}
	return bytes.TrimRight(block[:last.end], "\r\n"), nil
}

func tomlDeleteTable(doc []byte, table string) ([]byte, error) {
	block, found, err := locateTable(doc, table)
	if err != nil || !found {
		return doc, err
	}
	start := block.start
	rest := doc[block.end:]
	nextLine, _, _ := bytes.Cut(rest, []byte("\n"))
	blank, hasBlank := blankLineBefore(doc, start)
	if hasBlank && len(bytes.TrimSpace(nextLine)) == 0 {
		start = blank
		if len(rest) == 0 && !bytes.HasSuffix(doc, []byte("\n")) {
			head := bytes.TrimSuffix(doc[:start], []byte("\n"))
			start = len(bytes.TrimSuffix(head, []byte("\r")))
		}
	}
	return verifiedTOML(slices.Concat(doc[:start], rest))
}

func blankLineBefore(doc []byte, at int) (int, bool) {
	if at == 0 {
		return 0, false
	}
	start := bytes.LastIndexByte(doc[:at-1], '\n') + 1
	return start, len(bytes.TrimRight(doc[start:at], "\r\n")) == 0
}

func verifiedTOML(out []byte) ([]byte, error) {
	var decoded map[string]any
	if err := decodeTOML(out, &decoded); err != nil {
		return nil, fmt.Errorf("%w: result would be %w", ErrUnsupportedLayout, err)
	}
	return out, nil
}
