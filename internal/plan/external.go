package plan

import "path/filepath"

const ReasonExternal = "managed externally: "

func ExternalFor(external []string, path string) (string, bool) {
	path = filepath.Clean(path)
	for _, ext := range external {
		if !filepath.IsAbs(ext) {
			continue
		}
		ext = filepath.Clean(ext)
		if ext == path {
			return ext, true
		}
		if rel, err := filepath.Rel(ext, path); err == nil && filepath.IsLocal(rel) {
			return ext, true
		}
	}
	return "", false
}

func External(external []string, path string) bool {
	_, found := ExternalFor(external, path)
	return found
}
