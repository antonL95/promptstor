package library

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
)

type LayerItems struct {
	Layer Layer
	Items []Item
}

type Violation struct {
	ItemID ID
	Name   string
	Layer  Layer
	Reason string
}

type Merged struct{ Items []Item }

var rankedLayers = []Layer{LayerCompany, LayerTeam, LayerProject, LayerPersonal, LayerDevice}

func Merge(layers []LayerItems) (Merged, []Violation) {
	ranked := slices.Clone(layers)
	slices.SortStableFunc(ranked, func(a, b LayerItems) int { return compareLayers(a.Layer, b.Layer) })

	kept := map[ID]Item{}
	var violations []Violation
	for _, current := range ranked {
		for _, item := range current.Items {
			item.Layer = current.Layer
			earlier, found := kept[item.ID]
			switch {
			case found && isEnforced(earlier):
				violations = append(violations, lockViolation(earlier, item.Layer))
			case item.Removed:
				delete(kept, item.ID)
			default:
				kept[item.ID] = item
			}
		}
	}
	slices.SortFunc(violations, compareViolations)
	return Merged{Items: slices.SortedFunc(maps.Values(kept), compareItems)}, violations
}

func (m Merged) ForTool(tool string) []Item {
	return slices.DeleteFunc(slices.Clone(m.Items), func(item Item) bool { return !item.Targets.allows(tool) })
}

func (m Merged) Hash() string {
	hash := sha256.New()
	for _, item := range m.Items {
		hash.Write(fmt.Appendf(nil, "%q %s\n", item.ID, ContentHash(item)))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (t Targets) allows(tool string) bool {
	if slices.Contains(t.Exclude, tool) {
		return false
	}
	return len(t.Include) == 0 || slices.Contains(t.Include, tool)
}

func isEnforced(it Item) bool {
	return it.Locked && (it.Layer == LayerCompany || it.Layer == LayerTeam)
}

func lockViolation(locked Item, offending Layer) Violation {
	return Violation{
		ItemID: locked.ID,
		Name:   locked.Name,
		Layer:  offending,
		Reason: fmt.Sprintf("locked by %s", locked.Layer),
	}
}

func compareLayers(a, b Layer) int {
	return cmp.Or(cmp.Compare(layerRank(a), layerRank(b)), cmp.Compare(a, b))
}

func layerRank(layer Layer) int {
	if rank := slices.Index(rankedLayers, layer); rank >= 0 {
		return rank
	}
	return len(rankedLayers)
}

func kindRank(kind Kind) int {
	return slices.IndexFunc(layouts, func(candidate layout) bool { return candidate.kind == kind })
}

func compareItems(a, b Item) int {
	return cmp.Or(
		cmp.Compare(kindRank(a.Kind), kindRank(b.Kind)),
		cmp.Compare(a.Order, b.Order),
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.ID, b.ID),
	)
}

func compareViolations(a, b Violation) int {
	return cmp.Or(
		compareLayers(a.Layer, b.Layer),
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.ItemID, b.ItemID),
	)
}
