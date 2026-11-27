package plan

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/manifest"
)

const (
	lockWait        = 5 * time.Second
	snapshotReason  = "change"
	defaultFileMode = fs.FileMode(0o644)
)

type Applier struct {
	Now      func() time.Time
	LockWait time.Duration
}

func Apply(changes []FileChange, snap Snapshotter, m *manifest.Manifest, manifestPath, lockPath string) (ApplyResult, error) {
	return Applier{LockWait: lockWait}.Apply(changes, snap, m, manifestPath, lockPath)
}

func (a Applier) Apply(changes []FileChange, snap Snapshotter, m *manifest.Manifest, manifestPath, lockPath string) (ApplyResult, error) {
	if a.Now == nil {
		a.Now = time.Now
	}
	var result ApplyResult
	var pending []FileChange
	for _, c := range sortedChanges(changes) {
		switch {
		case c.Blocked != "":
			result.Skipped = append(result.Skipped, c)
		case c.Op != OpNoop || len(c.Record) > 0 || len(c.Removed) > 0 || len(c.Retagged) > 0:
			pending = append(pending, c)
		}
	}
	if len(pending) == 0 {
		return result, nil
	}
	release, err := fsx.Acquire(lockPath, a.LockWait)
	if err != nil {
		return result, fmt.Errorf("apply: %w", err)
	}
	defer release()
	if entries := snapshotEntries(pending); len(entries) > 0 {
		id, err := snap.Snapshot(snapshotReason, entries)
		if err != nil {
			return result, fmt.Errorf("snapshot before writing: %w", err)
		}
		result.SnapshotID = id
	}
	for _, c := range pending {
		if c.Op != OpNoop {
			if err := write(c); err != nil {
				return result, fmt.Errorf("apply %s: %w", c.Path, err)
			}
			result.Written = append(result.Written, c.Path)
		}
		if err := record(m, c, a.Now()); err != nil {
			return result, fmt.Errorf("record %s: %w", c.Path, err)
		}
		if err := m.Save(manifestPath); err != nil {
			return result, fmt.Errorf("record %s: %w", c.Path, err)
		}
	}
	return result, nil
}

func sortedChanges(changes []FileChange) []FileChange {
	sorted := slices.Clone(changes)
	slices.SortStableFunc(sorted, func(a, b FileChange) int { return cmp.Compare(a.Path, b.Path) })
	return sorted
}

func snapshotEntries(pending []FileChange) []SnapshotEntry {
	var entries []SnapshotEntry
	for _, c := range pending {
		if c.Op == OpNoop {
			continue
		}
		entry := SnapshotEntry{Tool: strings.Join(changeTools(c), ","), Path: c.Path, Root: c.Root}
		if c.SharedState && onlyJSON(c) {
			entry.Keys = jsonPointers(c)
		}
		entry.RedactKeys = secretKeys(c)
		entries = append(entries, entry)
	}
	return entries
}

func changeTools(c FileChange) []string {
	tools := toolsOf(c.Units)
	for _, e := range c.Removed {
		tools = append(tools, e.Tools()...)
	}
	slices.Sort(tools)
	return slices.Compact(tools)
}

func isJSONKind(kind manifest.UnitKind) bool {
	return kind == manifest.UnitJSON || kind == manifest.UnitJSONElement
}

func onlyJSON(c FileChange) bool {
	return !slices.ContainsFunc(c.Units, func(u Unit) bool { return !isJSONKind(u.Kind) }) &&
		!slices.ContainsFunc(c.Removed, func(e manifest.Entry) bool { return !isJSONKind(e.Unit) })
}

func jsonPointers(c FileChange) []string {
	var pointers []string
	add := func(kind manifest.UnitKind, key string) {
		if parsed, err := manifest.ParseKey(kind, key); err == nil {
			pointers = append(pointers, parsed.Pointer)
		}
	}
	for _, u := range c.Units {
		add(u.Kind, u.Key)
	}
	for _, e := range c.Removed {
		add(e.Unit, e.Key)
	}
	return sortedSet(pointers)
}

func secretKeys(c FileChange) []string {
	var keys []string
	for _, u := range c.Units {
		if u.Secret {
			keys = append(keys, u.Key)
		}
	}
	for _, e := range c.Removed {
		if e.Secret {
			keys = append(keys, e.Key)
		}
	}
	return sortedSet(keys)
}

func sortedSet(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	slices.Sort(values)
	return slices.Compact(values)
}

var errChangedSincePlan = errors.New("changed since the plan was made, run it again")

func write(c FileChange) error {
	if err := unchangedSincePlan(c); err != nil {
		return err
	}
	if c.ReplaceLink {
		if err := removeLink(c.Root, c.Path); err != nil {
			return err
		}
	}
	if c.Op == OpDelete {
		if err := fsx.Remove(c.Root, c.Path); err != nil {
			return err
		}
		return pruneEmptyParents(c.Root, c.Path)
	}
	return fsx.WriteAtomic(c.Root, c.Path, c.After, creationMode(c))
}

func unchangedSincePlan(c FileChange) error {
	current, err := os.ReadFile(c.Path)
	missing := errors.Is(err, fs.ErrNotExist)
	if err != nil && !missing {
		return fmt.Errorf("read %s: %w", c.Path, err)
	}
	existed := c.Exists && (!c.ReplaceLink || c.Before != nil)
	if missing == existed || !bytes.Equal(current, c.Before) {
		return errChangedSincePlan
	}
	return nil
}

func removeLink(root, path string) error {
	link, linked, err := fsx.LinkedAncestor(root, filepath.Dir(path))
	if err != nil {
		return err
	}
	if linked {
		return fmt.Errorf("%w: %s", fsx.ErrSymlink, link)
	}
	entry, err := fsx.Inspect(path)
	if err != nil || !entry.IsLink {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove link %s: %w", path, err)
	}
	return nil
}

func creationMode(c FileChange) fs.FileMode {
	for _, u := range c.Units {
		if u.Mode.Perm() != 0 {
			return u.Mode.Perm()
		}
	}
	return defaultFileMode
}

func pruneEmptyParents(root, path string) error {
	root = filepath.Clean(root)
	for dir := filepath.Dir(path); dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)); dir = filepath.Dir(dir) {
		entry, err := fsx.Inspect(dir)
		if err != nil || !entry.IsDir || entry.IsLink {
			return err
		}
		children, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("read %s: %w", dir, err)
		}
		if len(children) > 0 {
			return nil
		}
		if err := os.Remove(dir); err != nil {
			return fmt.Errorf("remove empty %s: %w", dir, err)
		}
	}
	return nil
}

func record(m *manifest.Manifest, c FileChange, now time.Time) error {
	for _, e := range c.Removed {
		m.Remove(e.Path, e.Unit, e.Key)
	}
	for _, e := range c.Retagged {
		m.Put(e)
	}
	units := c.Record
	if c.Op != OpNoop {
		units = c.Units
	}
	for _, u := range units {
		hash, err := manifest.Hash(u.Kind, u.Content)
		if err != nil {
			return err
		}
		m.Put(manifest.Entry{
			Tool:    u.Tool,
			Path:    u.Path,
			Root:    u.Root,
			Unit:    u.Kind,
			Key:     u.Key,
			Hash:    hash,
			Items:   u.Items,
			Secret:  u.Secret,
			Written: now.UTC(),
		})
	}
	return nil
}
