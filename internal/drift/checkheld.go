package drift

import (
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

type unitKey struct {
	path string
	unit manifest.UnitKind
	key  string
}

func (c checker) held(changes []plan.FileChange) ([]Finding, error) {
	covered := map[unitKey]bool{}
	for _, fc := range changes {
		for _, u := range fc.Units {
			covered[unitKey{u.Path, u.Kind, u.Key}] = true
		}
		for _, e := range slices.Concat(fc.Removed, fc.Retagged) {
			covered[unitKey{e.Path, e.Unit, e.Key}] = true
		}
	}
	roots := map[string]bool{}
	for _, t := range c.targets {
		roots[targetRoot(t)] = true
	}
	byPath := map[string][]manifest.Entry{}
	for _, e := range c.manifest.Entries {
		if roots[e.Root] && !e.Adopted && !covered[unitKey{e.Path, e.Unit, e.Key}] {
			byPath[e.Path] = append(byPath[e.Path], e)
		}
	}
	var findings []Finding
	for _, path := range slices.Sorted(maps.Keys(byPath)) {
		fc, readable, err := readHeld(path)
		if err != nil {
			return nil, err
		}
		if !readable {
			continue
		}
		for _, e := range byPath[path] {
			have, onDisk, err := diskHash(fc, e.Unit, e.Key)
			if err != nil {
				return nil, err
			}
			switch {
			case !onDisk:
				findings = append(findings, c.unitFinding(KindMissing, e.Tool, e.Path, e.Key, e.Items, e.Secret, detailNotOnDisk))
			case have != e.Hash:
				detail := c.handEditDetail(e.Tool, e.Root, e.Path, e.Unit)
				findings = append(findings, c.unitFinding(KindHandEdit, e.Tool, e.Path, e.Key, e.Items, e.Secret, detail))
			}
		}
	}
	return findings, nil
}

func readHeld(path string) (plan.FileChange, bool, error) {
	entry, err := fsx.Inspect(path)
	if err != nil {
		return plan.FileChange{}, false, fmt.Errorf("check %s: %w", path, err)
	}
	if !entry.Exists {
		return plan.FileChange{Path: path}, true, nil
	}
	if entry.IsLink || !entry.Mode.IsRegular() {
		return plan.FileChange{}, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return plan.FileChange{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	return plan.FileChange{Path: path, Before: data, Exists: true}, true, nil
}
