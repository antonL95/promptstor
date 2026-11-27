package cursor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/antonL95/promptstor/internal/managed"
)

const (
	frontmatterFence = "---"
	byteOrderMark    = "\xef\xbb\xbf"
)

var errUnclosed = errors.New("frontmatter has no closing --- line")

type header struct {
	lines bytes.Buffer
	err   error
}

func newHeader() *header { return &header{} }

func (h *header) set(key string, value any) {
	keyNode, valueNode := &yaml.Node{}, &yaml.Node{}
	keyNode.SetString(key)
	if err := valueNode.Encode(value); err != nil {
		h.err = cmp.Or(h.err, fmt.Errorf("frontmatter %s: %w", key, err))
		return
	}
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{keyNode, valueNode}}
	encoder := yaml.NewEncoder(&h.lines)
	encoder.SetIndent(2)
	if err := cmp.Or(encoder.Encode(mapping), encoder.Close()); err != nil {
		h.err = cmp.Or(h.err, fmt.Errorf("frontmatter %s: %w", key, err))
	}
}

func (h *header) setUnquoted(key, value string) {
	h.lines.WriteString(key + ": " + value + "\n")
}

func (h *header) markdown(body string) ([]byte, error) {
	if h.err != nil {
		return nil, h.err
	}
	document := frontmatterFence + "\n" + h.lines.String() + frontmatterFence + "\n" + body
	return managed.WithNotice([]byte(document)), nil
}

func splitFrontmatter(data []byte) (lines []string, body string, err error) {
	text := strings.TrimPrefix(string(data), byteOrderMark)
	all := strings.SplitAfter(text, "\n")
	if !isFence(all[0]) {
		return nil, text, nil
	}
	for i := 1; i < len(all); i++ {
		if isFence(all[i]) {
			for _, line := range all[1:i] {
				lines = append(lines, strings.TrimRight(line, "\r\n"))
			}
			return lines, strings.Join(all[i+1:], ""), nil
		}
	}
	return nil, "", errUnclosed
}

func isFence(line string) bool { return strings.TrimRight(line, "\r\n") == frontmatterFence }

func decodeFields(lines []string) (map[string]any, error) {
	fields := map[string]any{}
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &fields); err != nil {
		return nil, err
	}
	return fields, nil
}
