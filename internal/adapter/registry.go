package adapter

import (
	"maps"
	"slices"
	"sync"
)

var registry = struct {
	sync.RWMutex
	adapters map[ToolID]Adapter
}{adapters: map[ToolID]Adapter{}}

func Register(a Adapter) {
	registry.Lock()
	defer registry.Unlock()
	registry.adapters[a.ID()] = a
}

func All() []Adapter {
	registry.RLock()
	defer registry.RUnlock()
	all := make([]Adapter, 0, len(registry.adapters))
	for _, id := range slices.Sorted(maps.Keys(registry.adapters)) {
		all = append(all, registry.adapters[id])
	}
	return all
}

func Get(id ToolID) (Adapter, bool) {
	registry.RLock()
	defer registry.RUnlock()
	a, ok := registry.adapters[id]
	return a, ok
}
