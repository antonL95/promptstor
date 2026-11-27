package backup

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/antonL95/promptstor/internal/fsx"
)

func (r *restorer) pruneAll(files, all []SnapFile) error {
	listed := make(map[string]bool, len(all))
	for _, f := range all {
		listed[f.Path] = true
	}
	var extra []string
	for _, f := range files {
		if f.Kind != KindDir || f.Keys != nil {
			continue
		}
		found, err := r.unlisted(f.Path, listed)
		if err != nil {
			return err
		}
		extra = append(extra, found...)
	}
	slices.Sort(extra)
	for _, path := range slices.Backward(slices.Compact(extra)) {
		r.act(path, OpPrune, "")
		if err := r.remove(path); err != nil {
			return fmt.Errorf("prune %s: %w", path, err)
		}
	}
	return nil
}

func (r *restorer) unlisted(root string, listed map[string]bool) ([]string, error) {
	_, linked, err := fsx.LinkedAncestor(r.store.Env.Home, root)
	if err != nil || linked {
		return nil, nil
	}
	current, err := fsx.Inspect(root)
	if err != nil || !current.IsDir {
		return nil, err
	}
	var candidates []string
	kept := make(map[string]bool)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if Excluded(r.store.Env, path) {
			for dir := filepath.Dir(path); dir != root && within(root, dir); dir = filepath.Dir(dir) {
				kept[dir] = true
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path != root && !listed[path] {
			candidates = append(candidates, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return slices.DeleteFunc(candidates, func(path string) bool { return kept[path] }), nil
}
