package policy

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/library"
)

const (
	projectDir = ".promptstor"
	acksName   = "acks.json"
)

func LoadProjectAcks(root string) (Acks, error) {
	path := filepath.Join(root, projectDir, acksName)
	entry, err := fsx.Inspect(path)
	if err != nil || !entry.Exists {
		return Acks{}, err
	}
	link, linked, err := fsx.LinkedAncestor(root, path)
	if err != nil {
		return Acks{}, fmt.Errorf("load acks %s: %w", path, err)
	}
	if linked {
		return Acks{}, fmt.Errorf("load acks %s: %s is symlinked, which is never followed", path, link)
	}
	return LoadAcks(path)
}

func (a Acks) WithProject(project Acks, items []library.Item) Acks {
	own := map[library.ID]bool{}
	for _, it := range items {
		if it.Layer == library.LayerProject {
			own[it.ID] = true
		}
	}
	merged := Acks{List: slices.Clone(a.List)}
	for _, x := range project.List {
		if own[x.ItemID] {
			merged.List = append(merged.List, x)
		}
	}
	return merged
}

func (a Acks) Acknowledge(u adapter.Unsupported, item library.Item, tool string) adapter.Unsupported {
	if u.Blocking && a.Has(u.ItemID, tool, library.ContentHash(item)) {
		u.Reason = acknowledgedPrefix + u.Reason
		u.Blocking = false
	}
	return u
}
