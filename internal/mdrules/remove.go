package mdrules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
)

type Block struct{ Begin, End string }

type RemoveOptions struct {
	Opaque []Block
	Keep   func(line string) bool
}

type span struct {
	name  string
	order int
	lines []int
	body  []string
}

type spanner struct {
	opt    RemoveOptions
	kept   []bool
	spans  []span
	open   Fence
	opaque int
	inside bool
}

func Remove(doc []byte, source string, rules []library.Item, opt RemoveOptions) ([]byte, error) {
	lines := strings.SplitAfter(string(doc), "\n")
	s := &spanner{opt: opt, kept: make([]bool, len(lines)), spans: []span{{name: fileName(source)}}, opaque: -1}
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r\n")
		if i == 0 {
			line = strings.TrimPrefix(line, byteOrderMark)
		}
		s.visit(i, line)
	}
	s.number()
	drop := make([]bool, len(lines))
	for _, it := range rules {
		at := slices.IndexFunc(s.spans, func(sp span) bool { return sp.name == it.Name && sp.order == it.Order })
		if at < 0 || normalLines(s.spans[at].body) != normalLines(strings.Split(it.Body, "\n")) {
			return nil, fmt.Errorf("rule %q is not where it was imported from in %s", it.Name, source)
		}
		for _, i := range s.spans[at].lines {
			drop[i] = !s.kept[i]
		}
	}
	var out strings.Builder
	for i, raw := range lines {
		if !drop[i] {
			out.WriteString(raw)
		}
	}
	return []byte(out.String()), nil
}

func (s *spanner) visit(i int, line string) {
	trimmed := strings.TrimRight(line, " \t")
	switch {
	case s.opaque >= 0:
		s.kept[i] = true
		if strings.TrimSpace(line) == s.opt.Opaque[s.opaque].End {
			s.opaque = -1
		}
		return
	case !s.open.IsOpen() && s.opensBlock(line):
		s.kept[i] = true
		return
	case trimmed == managed.Begin, trimmed == managed.End, trimmed == managed.Notice:
		s.inside = trimmed == managed.Begin || (s.inside && trimmed == managed.Notice)
		s.kept[i] = true
		return
	case !s.inside && !s.open.IsOpen() && s.opt.Keep != nil && s.opt.Keep(line):
		s.kept[i] = true
		return
	}
	s.kept[i] = s.inside
	if name, ok := heading(line); ok && !s.open.IsOpen() {
		s.spans = append(s.spans, span{name: name})
	}
	s.open = s.open.After(line)
	last := &s.spans[len(s.spans)-1]
	last.lines = append(last.lines, i)
	last.body = append(last.body, line)
}

func (s *spanner) opensBlock(line string) bool {
	for at, block := range s.opt.Opaque {
		if strings.TrimSpace(line) == block.Begin {
			s.opaque = at
			return true
		}
	}
	return false
}

func (s *spanner) number() {
	n := 0
	for i := range s.spans {
		if joinTrimmed(s.spans[i].body) != "" {
			n++
			s.spans[i].order = n * orderStep
		}
	}
}

func normalLines(lines []string) string {
	var kept []string
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}
