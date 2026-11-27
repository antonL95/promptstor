package engine

import (
	"fmt"
	"slices"

	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

func (e Engine) ResolveSecret(tool, path, key string) ([]byte, error) {
	m, err := manifest.Load(e.Env.ManifestPath())
	if err != nil {
		return nil, err
	}
	at := slices.IndexFunc(m.Entries, func(entry manifest.Entry) bool {
		return entry.Path == path && entry.Key == key && entry.Secret
	})
	if at < 0 {
		return nil, fmt.Errorf("%s %s was not written by promptstor as a secret", path, key)
	}
	roots := []string{""}
	if root := m.Entries[at].Root; root != e.Env.Home {
		roots = append(roots, root)
	}
	for _, projectRoot := range roots {
		lib, err := e.scoped(projectRoot)
		if err != nil {
			return nil, err
		}
		d, err := e.desired(projectRoot, lib, m)
		if err != nil {
			return nil, err
		}
		if u, found := secretUnit(d.units, path, key); found {
			return u.Content, nil
		}
	}
	return nil, fmt.Errorf("resolve %s %s for %s: the secret is not available", path, key, tool)
}

func secretUnit(units []plan.Unit, path, key string) (plan.Unit, bool) {
	at := slices.IndexFunc(units, func(u plan.Unit) bool { return u.Path == path && u.Key == key && u.Secret })
	if at < 0 {
		return plan.Unit{}, false
	}
	return units[at], true
}
