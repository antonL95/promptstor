package cli

import (
	"fmt"
	"strings"
)

const diffContext = 3

type edit struct {
	op   byte
	line string
}

func unifiedDiff(path string, before, after []byte) string {
	edits := diffLines(splitLines(string(before)), splitLines(string(after)))
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", path, path)
	for _, h := range hunks(edits) {
		writeHunk(&out, edits, h[0], h[1])
	}
	return out.String()
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func diffLines(a, b []string) []edit {
	common := make([][]int, len(a)+1)
	for i := range common {
		common[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	var edits []edit
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			edits = append(edits, edit{' ', a[i]})
			i++
			j++
		case j < len(b) && (i == len(a) || common[i][j+1] > common[i+1][j]):
			edits = append(edits, edit{'+', b[j]})
			j++
		default:
			edits = append(edits, edit{'-', a[i]})
			i++
		}
	}
	return edits
}

func hunks(edits []edit) [][2]int {
	var ranges [][2]int
	for at, e := range edits {
		if e.op == ' ' {
			continue
		}
		start, end := max(0, at-diffContext), min(len(edits), at+diffContext+1)
		if n := len(ranges); n > 0 && start <= ranges[n-1][1] {
			ranges[n-1][1] = end
			continue
		}
		ranges = append(ranges, [2]int{start, end})
	}
	return ranges
}

func writeHunk(out *strings.Builder, edits []edit, start, end int) {
	aLine, bLine := 1, 1
	for _, e := range edits[:start] {
		if e.op != '+' {
			aLine++
		}
		if e.op != '-' {
			bLine++
		}
	}
	aCount, bCount := 0, 0
	for _, e := range edits[start:end] {
		if e.op != '+' {
			aCount++
		}
		if e.op != '-' {
			bCount++
		}
	}
	fmt.Fprintf(out, "@@ -%s +%s @@\n", hunkRange(aLine, aCount), hunkRange(bLine, bCount))
	for _, e := range edits[start:end] {
		out.WriteByte(e.op)
		out.WriteString(e.line)
		if !strings.HasSuffix(e.line, "\n") {
			out.WriteString("\n\\ No newline at end of file\n")
		}
	}
}

func hunkRange(line, count int) string {
	if count == 0 {
		line--
	}
	return fmt.Sprintf("%d,%d", line, count)
}
