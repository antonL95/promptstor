package plan

import (
	"fmt"
	"os"
	"slices"

	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/manifest"
)

func Reconcile(m *manifest.Manifest, paths []string) ([]manifest.Entry, error) {
	var removed []manifest.Entry
	for _, path := range slices.Compact(slices.Sorted(slices.Values(paths))) {
		doc, present, err := readOwned(path)
		if err != nil {
			return removed, err
		}
		for _, e := range m.ForPath(path) {
			if stillOwned(e, doc, present) {
				continue
			}
			m.Remove(e.Path, e.Unit, e.Key)
			removed = append(removed, e)
		}
	}
	return removed, nil
}

func readOwned(path string) ([]byte, bool, error) {
	entry, err := fsx.Inspect(path)
	if err != nil {
		return nil, false, err
	}
	if !entry.Exists || entry.IsLink || !entry.Mode.IsRegular() {
		return nil, false, nil
	}
	doc, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	return doc, true, nil
}

func stillOwned(e manifest.Entry, doc []byte, present bool) bool {
	current, found, err := extractUnit(e.Unit, doc, e.Key, present)
	if err != nil || !found {
		return false
	}
	hash, err := manifest.Hash(e.Unit, current)
	return err == nil && hash == e.Hash
}
