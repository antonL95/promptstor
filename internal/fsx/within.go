package fsx

import "path/filepath"

func Within(parents []string, path string) (string, bool) {
	path = filepath.Clean(path)
	for _, parent := range parents {
		if !filepath.IsAbs(parent) {
			continue
		}
		parent = filepath.Clean(parent)
		if parent == path {
			return parent, true
		}
		if rel, err := filepath.Rel(parent, path); err == nil && filepath.IsLocal(rel) {
			return parent, true
		}
	}
	return "", false
}
