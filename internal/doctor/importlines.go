package doctor

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/mdimport"
	"github.com/antonL95/promptstor/internal/mdrules"
)

const (
	boostBegin = "<laravel-boost-guidelines>"
	boostEnd   = "</laravel-boost-guidelines>"
)

var readerSyntax = map[string]mdimport.Syntax{
	adapter.ClaudeCode:  mdimport.Claude,
	adapter.GeminiCLI:   mdimport.Gemini,
	adapter.Antigravity: mdimport.Antigravity,
}

type importLine struct {
	line   int
	target string
	owners []*candidate
}

func removeOptions(reader string) mdrules.RemoveOptions {
	var opt mdrules.RemoveOptions
	if syn, ok := readerSyntax[reader]; ok {
		opt.Keep = func(line string) bool { return mdimport.OnlyImports(line, syn) }
	}
	if reader == adapter.ClaudeCode {
		opt.Opaque = []mdrules.Block{{Begin: boostBegin, End: boostEnd}}
	}
	return opt
}

func (d *doctor) pulledImports(s scopeImports, mv *move, local []*candidate, targets exportTargets) ([]importLine, error) {
	syn, ok := readerSyntax[mv.reader]
	if !ok {
		return nil, nil
	}
	doc, err := os.ReadFile(mv.path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", mv.path, err)
	}
	lines := strings.Split(string(doc), "\n")
	inside := managedLines(lines)
	found := mdimport.Find(doc, syn)
	perLine := map[int]int{}
	for _, imp := range found {
		perLine[imp.Line]++
	}
	var pulled []importLine
	for _, imp := range found {
		if perLine[imp.Line] != 1 || inside[imp.Line-1] || !mdimport.OnlyImports(lines[imp.Line-1], syn) {
			continue
		}
		target, ok := mdimport.TargetPath(d.e.Env, mv.path, imp, syn)
		if !ok || targets.owner[target] != "" {
			continue
		}
		if owners, complete := d.subtreeOwners(s, mv.reader, target, syn, local); complete {
			pulled = append(pulled, importLine{line: imp.Line, target: target, owners: owners})
		}
	}
	return pulled, nil
}

func (d *doctor) subtreeOwners(s scopeImports, reader, target string, syn mdimport.Syntax, local []*candidate) ([]*candidate, bool) {
	resolved, err := mdimport.Resolve(d.e.Env, target, syn)
	if err != nil || len(resolved.Unresolved) > 0 {
		return nil, false
	}
	var files []string
	for _, f := range resolved.Files {
		files = append(files, f.Path)
	}
	var owners []*candidate
	for _, it := range s.byTool[reader].Items {
		if !slices.Contains(files, it.Source) {
			continue
		}
		owner := ownerOf(it, local)
		if titleOnlyRule(it) || owner == nil {
			return nil, false
		}
		owners = append(owners, owner)
	}
	return owners, len(owners) > 0
}

func managedLines(lines []string) []bool {
	inside := make([]bool, len(lines))
	open := false
	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == managed.Begin {
			open = true
		}
		inside[i] = open
		if trimmed == managed.End {
			open = false
		}
	}
	return inside
}

func removeLines(doc []byte, numbers []int) []byte {
	if len(numbers) == 0 {
		return doc
	}
	lines := strings.SplitAfter(string(doc), "\n")
	var out []string
	dropped := false
	for i, line := range lines {
		if slices.Contains(numbers, i+1) {
			dropped = true
			continue
		}
		blank := strings.TrimSpace(line) == ""
		if dropped && blank && line != "" && (len(out) == 0 || strings.TrimSpace(out[len(out)-1]) == "") {
			continue
		}
		dropped = false
		out = append(out, line)
	}
	return []byte(strings.Join(out, ""))
}
