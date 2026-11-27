package doctor

import (
	"maps"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/library"
)

const (
	grantsName = "%s runtime grants"
	allowList  = "allow"
)

func (d *doctor) promote(grants []drift.Finding) ([]stagedItem, error) {
	if len(grants) == 0 {
		return nil, nil
	}
	merged, _, err := d.e.Library(d.opt.Project)
	if err != nil {
		return nil, err
	}
	type group struct {
		layer  library.Layer
		values []string
	}
	groups := map[string]*group{}
	for _, f := range grants {
		layer := library.LayerPersonal
		if d.opt.Project != "" && strings.HasPrefix(f.Path, d.opt.Project+"/") {
			layer = library.LayerProject
		}
		key := f.Tool + "\x00" + string(layer)
		if groups[key] == nil {
			groups[key] = &group{layer: layer}
		}
		groups[key].values = append(groups[key].values, f.Name)
	}
	var items []stagedItem
	for _, key := range slices.Sorted(maps.Keys(groups)) {
		g := groups[key]
		tool, _, _ := strings.Cut(key, "\x00")
		dir, err := d.layerDir(g.layer)
		if err != nil {
			return nil, err
		}
		it := grantItem(merged.Items, tool, g.layer)
		native, _ := it.Native[tool].(map[string]any)
		native = maps.Clone(native)
		if native == nil {
			native = map[string]any{}
		}
		allowed, _ := native[allowList].([]any)
		allowed = slices.Clone(allowed)
		for _, value := range g.values {
			if !slices.Contains(allowed, any(value)) {
				allowed = append(allowed, value)
			}
		}
		native[allowList] = allowed
		it.Native = maps.Clone(it.Native)
		if it.Native == nil {
			it.Native = map[string]any{}
		}
		it.Native[tool] = native
		items = append(items, stagedItem{dir: dir, it: it})
	}
	return items, nil
}

func grantItem(items []library.Item, tool string, layer library.Layer) library.Item {
	name := strings.Replace(grantsName, "%s", tool, 1)
	at := slices.IndexFunc(items, func(it library.Item) bool {
		return it.Kind == library.KindPermission && it.Name == name && it.Layer == layer
	})
	if at >= 0 {
		return items[at]
	}
	return library.Item{
		ID:      library.NewID(),
		Kind:    library.KindPermission,
		Name:    name,
		Targets: library.Targets{Include: []string{tool}},
	}
}
