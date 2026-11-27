package drift

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
)

type occurrence struct {
	kind                    library.Kind
	name, key, source, body string
	tools                   []string
}

func CompareTools(imports map[string]adapter.Imported) []Finding {
	var findings []Finding
	var all []*occurrence
	tools := slices.Sorted(maps.Keys(imports))
	for _, tool := range tools {
		imported := imports[tool]
		for _, u := range imported.Unresolved {
			findings = append(findings, unresolvedFinding(tool, u))
		}
		for _, it := range imported.Items {
			if body := ruleBody(it); !titleOnly(body) {
				all = addOccurrence(all, tool, it, body)
			}
		}
	}
	for _, o := range all {
		findings = append(findings, compareOccurrence(o, all, tools)...)
	}
	return sorted(findings)
}

func addOccurrence(all []*occurrence, tool string, it library.Item, body string) []*occurrence {
	key := normalName(it.Name)
	for _, o := range all {
		if o.kind == it.Kind && o.source == it.Source && o.key == key {
			if !slices.Contains(o.tools, tool) {
				o.tools = append(o.tools, tool)
			}
			return all
		}
	}
	return append(all, &occurrence{
		kind:   it.Kind,
		name:   strings.TrimSpace(it.Name),
		key:    key,
		source: it.Source,
		body:   body,
		tools:  []string{tool},
	})
}

func compareOccurrence(o *occurrence, all []*occurrence, tools []string) []Finding {
	var lacking []string
	var counterparts []*occurrence
	for _, tool := range tools {
		if slices.Contains(o.tools, tool) || !supports(tool, o.kind) {
			continue
		}
		held := slices.DeleteFunc(slices.Clone(all), func(other *occurrence) bool {
			return other.kind != o.kind || !slices.Contains(other.tools, tool)
		})
		if slices.ContainsFunc(held, o.sameContent) {
			continue
		}
		named := slices.DeleteFunc(held, func(other *occurrence) bool { return other.key != o.key })
		if len(named) == 0 {
			lacking = append(lacking, tool)
			continue
		}
		counterparts = append(counterparts, named...)
	}
	var findings []Finding
	if len(counterparts) > 0 {
		if reference := slices.MinFunc(counterparts, rankOccurrences); rankOccurrences(reference, o) < 0 {
			findings = append(findings, o.finding(KindDiffers, fmt.Sprintf("differs from %s (%s)", strings.Join(reference.tools, ", "), reference.source)))
		}
	}
	if len(lacking) > 0 {
		findings = append(findings, o.finding(KindOnlyIn, fmt.Sprintf("%s not in %s", o.kind, strings.Join(lacking, ", "))))
	}
	return findings
}

func (o *occurrence) sameContent(other *occurrence) bool {
	return other.body == o.body && (o.kind == library.KindRule || other.key == o.key)
}

func (o *occurrence) finding(kind FindingKind, detail string) Finding {
	return Finding{Kind: kind, Tool: strings.Join(o.tools, ","), Name: o.name, Path: o.source, Detail: detail}
}

func rankOccurrences(a, b *occurrence) int {
	return cmp.Or(
		cmp.Compare(len(b.tools), len(a.tools)),
		cmp.Compare(a.tools[0], b.tools[0]),
		cmp.Compare(a.source, b.source),
	)
}

func supports(tool string, kind library.Kind) bool {
	a, found := adapter.Get(tool)
	return !found || a.Capabilities().Kinds[kind]
}

func unresolvedFinding(tool string, u adapter.Unresolved) Finding {
	return Finding{
		Kind:   KindUnresolvedImport,
		Tool:   tool,
		Name:   u.Target,
		Path:   u.File,
		Detail: fmt.Sprintf("line %s: %s", u.Line, u.Reason),
	}
}

func ruleBody(it library.Item) string {
	body := normalBody(it.Body)
	if it.Kind != library.KindRule {
		return body
	}
	first, rest, _ := strings.Cut(body, "\n")
	if heading, found := strings.CutPrefix(first, "## "); found && normalName(heading) == normalName(it.Name) {
		return normalBody(rest)
	}
	return body
}

func titleOnly(body string) bool {
	for line := range strings.Lines(body) {
		if !strings.HasPrefix(line, "#") {
			return false
		}
	}
	return true
}

func normalName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func normalBody(body string) string {
	var lines []string
	blank := false
	for line := range strings.Lines(strings.ReplaceAll(body, "\r\n", "\n")) {
		line = strings.TrimRight(line, " \t\r\n")
		if line == "" {
			blank = len(lines) > 0
			continue
		}
		if blank {
			lines = append(lines, "")
			blank = false
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
