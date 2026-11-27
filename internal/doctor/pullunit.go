package doctor

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

var errNotOwned = errors.New("PromptStor has no record of writing it")

func (d *doctor) pullUnit(f drift.Finding) ([]stagedItem, error) {
	entry, err := d.ownedEntry(f)
	if err != nil {
		return nil, err
	}
	imported, err := d.reimport(entry)
	if err != nil {
		return nil, err
	}
	merged, _, err := d.e.Library(d.opt.Project)
	if err != nil {
		return nil, err
	}
	var items []stagedItem
	for _, id := range entry.Items {
		at := slices.IndexFunc(merged.Items, func(it library.Item) bool { return it.ID == id })
		if at < 0 {
			return nil, fmt.Errorf("item %s is not in the library", id)
		}
		current := merged.Items[at]
		match, err := matching(current, imported)
		if err != nil {
			return nil, err
		}
		dir, err := d.layerDir(current.Layer)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", current.Kind, current.Name, err)
		}
		updated := current
		updated.Body = match.Body
		if match.Description != "" {
			updated.Description = match.Description
		}
		if current.Kind == library.KindSkill {
			updated.Files = maps.Clone(match.Files)
		}
		items = append(items, stagedItem{dir: dir, it: updated})
	}
	return items, nil
}

func (d *doctor) ownedEntry(f drift.Finding) (manifest.Entry, error) {
	m, err := manifest.Load(d.e.Env.ManifestPath())
	if err != nil {
		return manifest.Entry{}, err
	}
	at := slices.IndexFunc(m.Entries, func(e manifest.Entry) bool {
		return e.Path == f.Path && (f.Item == "" || slices.Contains(e.Items, f.Item))
	})
	if at < 0 {
		return manifest.Entry{}, errNotOwned
	}
	return m.Entries[at], nil
}

func (d *doctor) reimport(entry manifest.Entry) ([]library.Item, error) {
	targets, err := d.e.Targets(d.opt.Project)
	if err != nil {
		return nil, err
	}
	at := slices.IndexFunc(targets, func(t adapter.Target) bool { return scopeRoot(t) == entry.Root })
	if at < 0 {
		return nil, fmt.Errorf("no scope holds %s", entry.Path)
	}
	t := targets[at]
	section := ""
	if entry.Unit == manifest.UnitSection {
		doc, err := os.ReadFile(entry.Path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Path, err)
		}
		content, found, err := plan.ExtractUnit(entry.Unit, doc, entry.Key, true)
		if err != nil || !found {
			return nil, errors.Join(fmt.Errorf("%s has no managed section", entry.Path), err)
		}
		section = normalText(string(content))
	}
	var imported []library.Item
	for _, tool := range entry.Tools() {
		a, ok := d.adapter(tool)
		if !ok || !slices.Contains(t.Selected, tool) {
			continue
		}
		result, err := a.Import(t)
		if err != nil {
			return nil, fmt.Errorf("import %s: %w", tool, err)
		}
		for _, it := range result.Items {
			if fromPath(it, entry.Path) && (section == "" || strings.Contains(section, normalText(it.Body))) {
				imported = append(imported, it)
			}
		}
	}
	return imported, nil
}

func fromPath(it library.Item, path string) bool {
	return it.Source == path || strings.HasPrefix(path, it.Source+string(filepath.Separator))
}

func matching(current library.Item, imported []library.Item) (library.Item, error) {
	var found []library.Item
	for _, it := range imported {
		if it.Kind != current.Kind || normalName(it.Name) != normalName(current.Name) {
			continue
		}
		if !slices.ContainsFunc(found, func(other library.Item) bool { return other.Body == it.Body }) {
			found = append(found, it)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return library.Item{}, fmt.Errorf("%s %q is no longer in the file", current.Kind, current.Name)
	default:
		return library.Item{}, fmt.Errorf("%s %q appears %d times in the file", current.Kind, current.Name, len(found))
	}
}

func normalText(text string) string {
	var lines []string
	for line := range strings.Lines(strings.ReplaceAll(text, "\r\n", "\n")) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
