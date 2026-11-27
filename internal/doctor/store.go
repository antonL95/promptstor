package doctor

import (
	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
)

func scopeLayer(scope adapter.Scope) library.Layer {
	if scope == adapter.ScopeProject {
		return library.LayerProject
	}
	return library.LayerPersonal
}

func (d *doctor) storeLibrary(cands []*candidate) error {
	existing := map[string]library.ID{}
	used := map[string]bool{}
	loaded := map[string]bool{}
	for _, c := range keptCandidates(cands) {
		layer := scopeLayer(c.target.Scope)
		dir, err := d.layerDir(layer)
		if err != nil {
			return err
		}
		if !loaded[dir] {
			loaded[dir] = true
			items, err := library.LoadLayer(dir, layer, library.LoadOptions{})
			if err != nil {
				return err
			}
			for _, it := range items {
				if path, err := library.ItemPath(dir, it); err == nil {
					existing[path] = it.ID
				}
			}
		}
		path, err := library.ItemPath(dir, c.it)
		if err != nil {
			d.w.printf("not pulled: %v\n", err)
			c.drop = true
			continue
		}
		if used[path] {
			d.w.printf("not pulled: %s %q has the same file name as another pulled item\n", c.it.Kind, c.it.Name)
			c.drop = true
			continue
		}
		used[path] = true
		if id, found := existing[path]; found {
			c.it.ID = id
		}
		c.it.Layer = layer
		if d.opt.DryRun {
			c.stored = true
			continue
		}
		if _, err := library.WriteItem(dir, c.it); err != nil {
			d.w.printf("not pulled: %v\n", err)
			c.drop = true
			continue
		}
		c.stored = true
	}
	return nil
}

func storedItems(cands []*candidate) []library.Item {
	var items []library.Item
	for _, c := range cands {
		if c.kept() && c.stored {
			items = append(items, c.it)
		}
	}
	return items
}
