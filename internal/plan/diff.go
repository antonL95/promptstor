package plan

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/manifest"
)

const (
	reasonNoRoot     = "no root known for this file"
	reasonUnmanaged  = "not created by promptstor"
	reasonConflict   = "shared file conflict: "
	reasonSymlinked  = "symlinked -> "
	reasonNotRegular = "not a regular file"
)

type unitKey struct {
	path string
	kind manifest.UnitKind
	key  string
}

type fileDraft struct {
	path, root string
	units      []Unit
	removed    []manifest.Entry
	retagged   []manifest.Entry
	blocked    string
}

func Diff(desired []Unit, m manifest.Manifest, tools []string, opt Options) ([]FileChange, error) {
	if err := validate(desired); err != nil {
		return nil, err
	}
	drafts := map[string]*fileDraft{}
	draftFor := func(path string) *fileDraft {
		if drafts[path] == nil {
			drafts[path] = &fileDraft{path: path}
		}
		return drafts[path]
	}
	groups := map[unitKey][]Unit{}
	for _, u := range desired {
		key := unitKey{u.Path, u.Kind, u.Key}
		groups[key] = append(groups[key], u)
	}
	for _, key := range slices.SortedFunc(maps.Keys(groups), compareUnitKeys) {
		draft := draftFor(key.path)
		merged, conflict := merge(groups[key])
		draft.root = merged.Root
		draft.units = append(draft.units, merged)
		if conflict && draft.blocked == "" {
			draft.blocked = reasonConflict + strings.Join(toolsOf(groups[key]), ", ")
		}
	}
	for _, e := range m.Entries {
		if _, wanted := groups[unitKey{e.Path, e.Unit, e.Key}]; wanted || kept(opt.Keep, e) {
			continue
		}
		remaining := slices.DeleteFunc(e.Tools(), func(tool string) bool { return slices.Contains(tools, tool) })
		if len(remaining) == len(e.Tools()) {
			continue
		}
		draft := draftFor(e.Path)
		if draft.root == "" {
			draft.root = e.Root
		}
		if len(remaining) == 0 {
			draft.removed = append(draft.removed, e)
			continue
		}
		e.Tool = strings.Join(remaining, ",")
		draft.retagged = append(draft.retagged, e)
	}
	changes := make([]FileChange, 0, len(drafts))
	for _, path := range slices.Sorted(maps.Keys(drafts)) {
		draft := drafts[path]
		slices.SortFunc(draft.removed, compareEntries)
		slices.SortFunc(draft.retagged, compareEntries)
		blockMixedFile(draft)
		changes = append(changes, planFile(draft, &m, opt))
	}
	return changes, nil
}

func kept(keep []manifest.Entry, e manifest.Entry) bool {
	return slices.ContainsFunc(keep, func(k manifest.Entry) bool {
		return k.Path == e.Path && k.Unit == e.Unit && k.Key == e.Key
	})
}

func validate(desired []Unit) error {
	for _, u := range desired {
		if err := validateUnit(u); err != nil {
			return fmt.Errorf("unit %s %q in %s from %q: %w", u.Kind, u.Key, u.Path, u.Tool, err)
		}
	}
	roots := map[string]string{}
	for _, u := range desired {
		if root, seen := roots[u.Path]; seen && root != u.Root {
			return fmt.Errorf("units for %s name two roots, %s and %s", u.Path, root, u.Root)
		}
		roots[u.Path] = u.Root
	}
	return nil
}

func validateUnit(u Unit) error {
	if u.Tool == "" {
		return errors.New("no tool")
	}
	if !filepath.IsAbs(u.Path) || filepath.Clean(u.Path) != u.Path {
		return errors.New("path is not absolute and clean")
	}
	if !filepath.IsAbs(u.Root) || filepath.Clean(u.Root) != u.Root {
		return errors.New("root is not absolute and clean")
	}
	if relative, err := filepath.Rel(u.Root, u.Path); err != nil || relative == "." || !filepath.IsLocal(relative) {
		return fmt.Errorf("path is not inside %s", u.Root)
	}
	if _, err := manifest.ParseKey(u.Kind, u.Key); err != nil {
		return err
	}
	if _, err := manifest.Hash(u.Kind, u.Content); err != nil {
		return err
	}
	return nil
}

func merge(group []Unit) (Unit, bool) {
	merged := group[0]
	conflict := false
	var items []library.ID
	for _, u := range group {
		if !bytes.Equal(u.Content, merged.Content) || u.Mode != merged.Mode {
			conflict = true
		}
		merged.Secret = merged.Secret || u.Secret
		items = append(items, u.Items...)
	}
	slices.Sort(items)
	merged.Tool = strings.Join(toolsOf(group), ",")
	merged.Items = slices.Compact(items)
	merged.Content = slices.Clone(merged.Content)
	return merged, conflict
}

func toolsOf(units []Unit) []string {
	var tools []string
	for _, u := range units {
		tools = append(tools, strings.Split(u.Tool, ",")...)
	}
	slices.Sort(tools)
	return slices.Compact(tools)
}

func blockMixedFile(draft *fileDraft) {
	if draft.blocked != "" || len(draft.units) < 2 {
		return
	}
	if slices.ContainsFunc(draft.units, func(u Unit) bool { return u.Kind == manifest.UnitFile }) {
		draft.blocked = reasonConflict + strings.Join(toolsOf(draft.units), ", ")
	}
}

func planFile(draft *fileDraft, m *manifest.Manifest, opt Options) FileChange {
	c := FileChange{
		Path:        draft.path,
		Root:        draft.root,
		Units:       draft.units,
		Removed:     draft.removed,
		Retagged:    draft.retagged,
		Op:          OpNoop,
		SharedState: slices.Contains(opt.SharedState, draft.path),
	}
	if releasesOnly(draft) {
		return c
	}
	if draft.blocked != "" {
		return blocked(c, draft.blocked)
	}
	if c.Root == "" {
		return blocked(c, reasonNoRoot)
	}
	if reason := checkLinks(&c, opt.ReplaceSymlinks); reason != "" {
		return blocked(c, reason)
	}
	before, exists, err := readCurrent(c.Path, c.ReplaceLink)
	if err != nil {
		return blocked(c, err.Error())
	}
	c.Before, c.Exists = before, exists || c.ReplaceLink
	e := editor{change: &c, manifest: m, adopt: slices.Contains(opt.Adopt, c.Path), doc: slices.Clone(before), exists: exists}
	if reason := e.run(); reason != "" {
		return blocked(c, reason)
	}
	return c
}

func releasesOnly(draft *fileDraft) bool {
	return len(draft.units) == 0 && len(draft.removed) > 0 &&
		!slices.ContainsFunc(draft.removed, func(e manifest.Entry) bool { return !e.Adopted })
}

func blocked(c FileChange, reason string) FileChange {
	c.Blocked = reason
	c.Op = OpNoop
	c.After = nil
	c.Record = nil
	c.Adopted = nil
	return c
}

func checkLinks(c *FileChange, consented []string) string {
	link, linked, err := fsx.LinkedAncestor(c.Root, c.Path)
	if err != nil {
		return err.Error()
	}
	if !linked {
		return ""
	}
	entry, err := fsx.Inspect(link)
	if err != nil {
		return err.Error()
	}
	reason := reasonSymlinked + entry.LinkTarget
	if link != c.Path {
		return reason + " (parent directory " + link + ")"
	}
	if !slices.Contains(consented, c.Path) {
		return reason
	}
	c.ReplaceLink = true
	return ""
}

func readCurrent(path string, throughLink bool) ([]byte, bool, error) {
	if throughLink {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return data, err == nil, err
	}
	entry, err := fsx.Inspect(path)
	if err != nil || !entry.Exists {
		return nil, false, err
	}
	if !entry.Mode.IsRegular() {
		return nil, false, errors.New(reasonNotRegular)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	return data, true, nil
}

func compareUnitKeys(a, b unitKey) int {
	return cmp.Or(cmp.Compare(a.path, b.path), cmp.Compare(a.kind, b.kind), cmp.Compare(a.key, b.key))
}

func compareEntries(a, b manifest.Entry) int {
	return compareUnitKeys(unitKey{a.Path, a.Unit, a.Key}, unitKey{b.Path, b.Unit, b.Key})
}

func hasNotice(doc []byte) bool {
	return bytes.Contains(doc, []byte(managed.Notice))
}
