package mdimport

import (
	"strings"

	"github.com/antonL95/promptstor/internal/mdrules"
)

type Import struct {
	Line        int
	Token, Path string
}

func Find(doc []byte, syn Syntax) []Import {
	var found []Import
	scan(doc, func(number int, line string, inFence bool) {
		if inFence {
			return
		}
		found = append(found, lineImports(number, line, syn)...)
	})
	return found
}

func StripImportLines(doc []byte, syn Syntax) []byte {
	var kept []string
	lines := strings.SplitAfter(string(doc), "\n")
	dropped := false
	scan(doc, func(number int, line string, inFence bool) {
		if !inFence && OnlyImports(line, syn) {
			dropped = true
			return
		}
		blank := strings.TrimSpace(line) == ""
		if dropped && blank && (len(kept) == 0 || strings.TrimSpace(kept[len(kept)-1]) == "") {
			return
		}
		dropped = false
		kept = append(kept, lines[number-1])
	})
	return []byte(strings.Join(kept, ""))
}

func scan(doc []byte, visit func(number int, line string, inFence bool)) {
	text := string(doc)
	if text == "" {
		return
	}
	var open mdrules.Fence
	for i, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		line = strings.TrimSuffix(line, "\r")
		next := open.After(line)
		visit(i+1, line, open.IsOpen() || next.IsOpen())
		open = next
	}
}

func lineImports(number int, line string, syn Syntax) []Import {
	var found []Import
	pathGroup := syn.Line.SubexpIndex("path")
	for _, match := range syn.Line.FindAllStringSubmatchIndex(withoutCodeSpans(line), -1) {
		token := strings.TrimLeft(line[match[0]:match[1]], " \t")[1:]
		imported := Import{Line: number, Token: token}
		if start := match[2*pathGroup]; start >= 0 {
			imported.Path = strings.ReplaceAll(line[start:match[2*pathGroup+1]], `\ `, " ")
		}
		found = append(found, imported)
	}
	return found
}

func OnlyImports(line string, syn Syntax) bool {
	rest := strings.TrimSpace(withoutCodeSpans(line))
	if rest == "" {
		return false
	}
	return strings.TrimSpace(syn.Line.ReplaceAllString(rest, "")) == ""
}

func withoutCodeSpans(line string) string {
	masked := []byte(line)
	for at := 0; at < len(masked); {
		if masked[at] != '`' {
			at++
			continue
		}
		length := runLength(masked, at)
		end := closingRun(masked, at+length, length)
		if end < 0 {
			at += length
			continue
		}
		for i := at; i < end+length; i++ {
			masked[i] = ' '
		}
		at = end + length
	}
	return string(masked)
}

func runLength(text []byte, at int) int {
	length := 0
	for at+length < len(text) && text[at+length] == '`' {
		length++
	}
	return length
}

func closingRun(text []byte, from, length int) int {
	for at := from; at < len(text); {
		if text[at] != '`' {
			at++
			continue
		}
		run := runLength(text, at)
		if run == length {
			return at
		}
		at += run
	}
	return -1
}
