package drift

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
)

type itemKey struct {
	kind library.Kind
	name string
}

type occurrence struct {
	name, source, body string
	tools              []string
}

func CompareTools(imports map[string]adapter.Imported) []Finding {
	var findings []Finding
	groups := map[itemKey][]*occurrence{}
	tools := slices.Sorted(maps.Keys(imports))
	for _, tool := range tools {
		imported := imports[tool]
		for _, u := range imported.Unresolved {
			findings = append(findings, unresolvedFinding(tool, u))
		}
		for _, it := range imported.Items {
			if isPreamble(it) {
				continue
			}
			key := itemKey{kind: it.Kind, name: normalName(it.Name)}
			groups[key] = addOccurrence(groups[key], tool, it)
		}
	}
	for key, group := range groups {
		findings = append(findings, differences(group)...)
		findings = append(findings, onlyIn(key.kind, group, tools)...)
	}
	return sorted(findings)
}

func addOccurrence(group []*occurrence, tool string, it library.Item) []*occurrence {
	for _, o := range group {
		if o.source == it.Source {
			if !slices.Contains(o.tools, tool) {
				o.tools = append(o.tools, tool)
			}
			return group
		}
	}
	return append(group, &occurrence{
		name:   strings.TrimSpace(it.Name),
		source: it.Source,
		body:   normalBody(it.Body),
		tools:  []string{tool},
	})
}

func differences(group []*occurrence) []Finding {
	variants := map[string][]*occurrence{}
	for _, o := range group {
		variants[o.body] = append(variants[o.body], o)
	}
	if len(variants) < 2 {
		return nil
	}
	bodies := slices.SortedFunc(maps.Keys(variants), func(a, b string) int {
		return cmp.Or(
			cmp.Compare(toolCount(variants[b]), toolCount(variants[a])),
			compareOccurrences(first(variants[a]), first(variants[b])),
		)
	})
	reference := first(variants[bodies[0]])
	var findings []Finding
	for _, body := range bodies[1:] {
		for _, o := range variants[body] {
			findings = append(findings, Finding{
				Kind:   KindDiffers,
				Tool:   strings.Join(o.tools, ","),
				Name:   o.name,
				Path:   o.source,
				Detail: fmt.Sprintf("differs from %s (%s)", strings.Join(reference.tools, ", "), reference.source),
			})
		}
	}
	return findings
}

func onlyIn(kind library.Kind, group []*occurrence, tools []string) []Finding {
	var holders []string
	for _, o := range group {
		holders = append(holders, o.tools...)
	}
	var lacking []string
	for _, tool := range tools {
		if !slices.Contains(holders, tool) && supports(tool, kind) {
			lacking = append(lacking, tool)
		}
	}
	if len(lacking) == 0 {
		return nil
	}
	findings := make([]Finding, 0, len(group))
	for _, o := range group {
		findings = append(findings, Finding{
			Kind:   KindOnlyIn,
			Tool:   strings.Join(o.tools, ","),
			Name:   o.name,
			Path:   o.source,
			Detail: fmt.Sprintf("%s not in %s", kind, strings.Join(lacking, ", ")),
		})
	}
	return findings
}

func supports(tool string, kind library.Kind) bool {
	a, found := adapter.Get(tool)
	return !found || a.Capabilities().Kinds[kind]
}

func toolCount(group []*occurrence) int {
	count := 0
	for _, o := range group {
		count += len(o.tools)
	}
	return count
}

func first(group []*occurrence) *occurrence {
	return slices.MinFunc(group, compareOccurrences)
}

func compareOccurrences(a, b *occurrence) int {
	return cmp.Or(cmp.Compare(a.tools[0], b.tools[0]), cmp.Compare(a.source, b.source))
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

func isPreamble(it library.Item) bool {
	base := filepath.Base(it.Source)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return it.Kind == library.KindRule && it.Name == stem && stem == strings.ToUpper(stem)
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
