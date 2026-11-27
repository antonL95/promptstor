package library

import (
	"fmt"
	"path/filepath"
)

const skillFile = "SKILL.md"

type layout struct {
	kind      Kind
	dir       string
	extension string
	folder    bool
}

var layouts = []layout{
	{kind: KindRule, dir: "rules", extension: ".md"},
	{kind: KindSkill, dir: "skills", folder: true},
	{kind: KindCommand, dir: "commands", extension: ".md"},
	{kind: KindSubagent, dir: "subagents", extension: ".md"},
	{kind: KindPermission, dir: "permissions", extension: ".yaml"},
	{kind: KindHook, dir: "hooks", extension: ".yaml", folder: true},
	{kind: KindMCP, dir: "mcp", extension: ".yaml"},
}

func layoutOf(kind Kind) (layout, error) {
	for _, candidate := range layouts {
		if candidate.kind == kind {
			return candidate, nil
		}
	}
	return layout{}, fmt.Errorf("unknown kind %q", kind)
}

func (l layout) isYAML() bool { return l.extension == ".yaml" }

func (l layout) mainPath(dir, slug string) string {
	if l.kind == KindSkill {
		return filepath.Join(dir, l.dir, slug, skillFile)
	}
	return filepath.Join(dir, l.dir, slug+l.extension)
}

func (l layout) folderPath(dir, slug string) string {
	return filepath.Join(dir, l.dir, slug)
}

func writeSlug(it Item) string {
	switch {
	case it.Kind == KindSkill:
		return it.Name
	case it.slug != "":
		return it.slug
	default:
		return Slug(it.Name)
	}
}

func ItemPath(dir string, it Item) (string, error) {
	l, err := layoutOf(it.Kind)
	if err != nil {
		return "", fmt.Errorf("item %q: %w", it.Name, err)
	}
	slug := writeSlug(it)
	if slug == "" {
		return "", fmt.Errorf("%s %q: the name gives no file name", it.Kind, it.Name)
	}
	return l.mainPath(dir, slug), nil
}

func storedSlug(it Item) string {
	if it.slug != "" {
		return it.slug
	}
	return writeSlug(it)
}
