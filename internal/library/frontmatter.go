package library

import (
	"bytes"
	"errors"
)

const delimiter = "---"

var byteOrderMark = []byte("\ufeff")

func splitFrontmatter(data []byte) (header, body []byte, err error) {
	data = bytes.TrimPrefix(data, byteOrderMark)
	first, rest := cutLine(data)
	if !isDelimiter(first) {
		return nil, data, nil
	}
	for remaining := rest; len(remaining) > 0; {
		line, after := cutLine(remaining)
		if isDelimiter(line) {
			return rest[:len(rest)-len(remaining)], after, nil
		}
		remaining = after
	}
	return nil, nil, errors.New("frontmatter has no closing --- line")
}

func cutLine(data []byte) (line, rest []byte) {
	line, rest, _ = bytes.Cut(data, []byte("\n"))
	return line, rest
}

func isDelimiter(line []byte) bool {
	return string(bytes.TrimSuffix(line, []byte("\r"))) == delimiter
}

func joinFrontmatter(header []byte, body string) []byte {
	document := []byte(delimiter + "\n")
	document = append(document, header...)
	document = append(document, delimiter+"\n"...)
	return append(document, body...)
}
