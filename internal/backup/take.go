package backup

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/plan"
)

func (s Store) Snapshot(reason string, entries []plan.SnapshotEntry) (string, error) {
	snap, err := s.Take(Reason(reason), entries)
	return snap.ID, err
}

func (s Store) Take(reason Reason, entries []plan.SnapshotEntry) (Snapshot, error) {
	t := taker{store: s, blobs: make(map[string][]byte)}
	t.snap = Snapshot{Created: s.now(), Reason: reason, Entries: slices.Clone(entries)}
	for _, entry := range entries {
		if err := t.record(entry); err != nil {
			return Snapshot{}, fmt.Errorf("snapshot %s: %w", entry.Path, err)
		}
	}
	if err := s.save(&t.snap, t.blobs); err != nil {
		return Snapshot{}, err
	}
	return t.snap, nil
}

type taker struct {
	store Store
	snap  Snapshot
	blobs map[string][]byte
	root  string
}

func (t *taker) record(entry plan.SnapshotEntry) error {
	path := filepath.Clean(entry.Path)
	if !filepath.IsAbs(path) {
		return errors.New("path is not absolute")
	}
	if Excluded(t.store.Env, path) {
		if t.snap.Reason == ReasonChange {
			return errors.New("refusing to change a credential file that is never backed up")
		}
		t.snap.Skipped = append(t.snap.Skipped, path)
		return nil
	}
	current, err := fsx.Inspect(path)
	if err != nil {
		return err
	}
	t.root = entry.Root
	if len(entry.Keys) > 0 {
		return t.recordKeys(entry.Tool, path, current, entry.Keys, entry.RedactKeys)
	}
	switch {
	case !current.Exists:
		if err := t.recordMissingParents(entry.Tool, path); err != nil {
			return err
		}
		t.add(SnapFile{Tool: entry.Tool, Path: path, Kind: KindAbsent})
	case current.IsLink:
		t.add(SnapFile{Tool: entry.Tool, Path: path, Kind: KindSymlink, LinkTarget: current.LinkTarget})
	case current.IsDir:
		return t.recordDir(entry.Tool, path)
	case current.Mode.IsRegular():
		return t.recordFile(entry.Tool, path, current.Mode, entry.RedactKeys)
	default:
		t.snap.Skipped = append(t.snap.Skipped, path)
	}
	return nil
}

func (t *taker) recordMissingParents(tool, path string) error {
	root := filepath.Clean(cmp.Or(t.root, t.store.Env.Home))
	var missing []string
	for dir := filepath.Dir(path); dir != root && within(root, dir); dir = filepath.Dir(dir) {
		current, err := fsx.Inspect(dir)
		if err != nil {
			return err
		}
		if current.Exists {
			break
		}
		missing = append(missing, dir)
	}
	for _, dir := range slices.Backward(missing) {
		if !slices.ContainsFunc(t.snap.Files, func(f SnapFile) bool { return f.Path == dir }) {
			t.add(SnapFile{Tool: tool, Path: dir, Kind: KindAbsent})
		}
	}
	return nil
}

func (t *taker) add(f SnapFile) {
	f.Root = t.root
	t.snap.Files = append(t.snap.Files, f)
}

func (t *taker) recordFile(tool, path string, mode fs.FileMode, redact []string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	excluded := ExcludedPointers(t.store.Env, path)
	var redacted []string
	if len(excluded) > 0 || len(redact) > 0 {
		if content, redacted, err = sanitize(content, excluded, redact); err != nil {
			return fmt.Errorf("keep secrets out of %s: %w", path, err)
		}
	}
	sum := blobSum(content)
	t.blobs[sum] = content
	t.add(SnapFile{Tool: tool, Path: path, Kind: KindFile, Mode: mode, Blob: sum, Redacted: redacted})
	return nil
}

func (t *taker) recordKeys(tool, path string, current fsx.Entry, keys, redact []string) error {
	excluded := ExcludedPointers(t.store.Env, path)
	if !current.Exists {
		stored, err := extractKeys(nil, keys, excluded)
		t.add(SnapFile{Tool: tool, Path: path, Kind: KindAbsent, Keys: stored})
		return err
	}
	if current.IsLink || !current.Mode.IsRegular() {
		return fmt.Errorf("%s holds keys but is not a regular file", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	clean, redacted, err := sanitize(content, excluded, redact)
	if err != nil {
		return fmt.Errorf("keep secrets out of %s: %w", path, err)
	}
	stored, err := extractKeys(clean, keys, excluded)
	if err != nil {
		return fmt.Errorf("read keys of %s: %w", path, err)
	}
	t.add(SnapFile{Tool: tool, Path: path, Kind: KindFile, Mode: current.Mode, Keys: stored, Redacted: coveredBy(stored, redacted)})
	return nil
}

func (t *taker) recordDir(tool, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if Excluded(t.store.Env, path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read link %s: %w", path, err)
			}
			t.add(SnapFile{Tool: tool, Path: path, Kind: KindSymlink, LinkTarget: target})
		case d.IsDir():
			t.add(SnapFile{Tool: tool, Path: path, Kind: KindDir, Mode: info.Mode()})
		case info.Mode().IsRegular():
			return t.recordFile(tool, path, info.Mode(), nil)
		default:
			t.snap.Skipped = append(t.snap.Skipped, path)
		}
		return nil
	})
}
