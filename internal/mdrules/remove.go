package mdrules

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
)

type remover struct {
	wanted  [][]string
	rule    int
	at      int
	dropped bool
	inside  bool
}

func Remove(doc []byte, rules []library.Item) ([]byte, error) {
	ordered := slices.Clone(rules)
	slices.SortStableFunc(ordered, func(a, b library.Item) int { return cmp.Compare(a.Order, b.Order) })
	r := &remover{}
	for _, it := range ordered {
		var lines []string
		for _, line := range strings.Split(it.Body, "\n") {
			if line = strings.TrimRight(line, " \t\r"); line != "" {
				lines = append(lines, line)
			}
		}
		if len(lines) > 0 {
			r.wanted = append(r.wanted, lines)
		}
	}
	var out strings.Builder
	for index, raw := range strings.SplitAfter(string(doc), "\n") {
		line := strings.TrimRight(raw, " \t\r\n")
		if index == 0 {
			line = strings.TrimPrefix(line, byteOrderMark)
		}
		if !r.drops(line) {
			out.WriteString(raw)
		}
	}
	if r.rule < len(r.wanted) {
		return nil, fmt.Errorf("rule text %q is not in the file", r.wanted[r.rule][0])
	}
	return []byte(out.String()), nil
}

func (r *remover) drops(line string) bool {
	switch {
	case line == managed.Begin:
		r.inside = true
	case line == managed.End:
		r.inside = false
		r.dropped = false
		return false
	}
	if r.inside {
		r.dropped = false
		return false
	}
	if line == "" {
		return r.dropped
	}
	if r.rule < len(r.wanted) && line == r.wanted[r.rule][r.at] {
		r.at++
		if r.at == len(r.wanted[r.rule]) {
			r.rule++
			r.at = 0
		}
		r.dropped = true
		return true
	}
	r.dropped = false
	return false
}
