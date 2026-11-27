package backup

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
)

type Reason string

const (
	ReasonFirstRun   Reason = "first-run"
	ReasonChange     Reason = "change"
	ReasonPreRestore Reason = "pre-restore"
)

type FileKind string

const (
	KindFile    FileKind = "file"
	KindSymlink FileKind = "symlink"
	KindDir     FileKind = "dir"
	KindAbsent  FileKind = "absent"
)

type SnapFile struct {
	Tool       string            `json:"tool"`
	Root       string            `json:"root,omitempty"`
	Path       string            `json:"path"`
	Kind       FileKind          `json:"kind"`
	LinkTarget string            `json:"linkTarget,omitempty"`
	Mode       fs.FileMode       `json:"mode,omitempty"`
	Blob       string            `json:"blob,omitempty"`
	Keys       map[string]string `json:"keys"`
	Redacted   []string          `json:"redacted,omitempty"`
}

type Snapshot struct {
	ID      string               `json:"id"`
	Created time.Time            `json:"created"`
	Reason  Reason               `json:"reason"`
	Entries []plan.SnapshotEntry `json:"entries"`
	Files   []SnapFile           `json:"files"`
	Skipped []string             `json:"skipped,omitempty"`
}

type Store struct {
	Env     paths.Env
	Now     func() time.Time
	Resolve func(tool, path, key string) ([]byte, error)
}

const (
	snapshotFile = "snapshot.json"
	blobDir      = "blobs"
	idLayout     = "20060102T150405Z"
	idAttempts   = 16
	lockWait     = 5 * time.Second
)

func (s Store) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

func (s Store) blobPath(sum string) string {
	return filepath.Join(s.Env.BackupDir(), blobDir, sum)
}

func blobSum(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func (s Store) save(snap *Snapshot, blobs map[string][]byte) error {
	if err := os.MkdirAll(s.Env.BackupDir(), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", s.Env.BackupDir(), err)
	}
	for _, sum := range sortedKeys(blobs) {
		if err := s.writeBlob(sum, blobs[sum]); err != nil {
			return err
		}
	}
	dir, err := s.newSnapshotDir(snap)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("encode snapshot %s: %w", snap.ID, err)
	}
	path := filepath.Join(dir, snapshotFile)
	if err := fsx.WriteAtomic(s.Env.BackupDir(), path, append(data, '\n'), 0o600); err != nil {
		return errors.Join(err, os.Remove(dir))
	}
	return nil
}

func (s Store) writeBlob(sum string, content []byte) error {
	path := s.blobPath(sum)
	existing, err := fsx.Inspect(path)
	if err != nil || existing.Exists {
		return err
	}
	return fsx.WriteAtomic(s.Env.BackupDir(), path, content, 0o600)
}

func (s Store) newSnapshotDir(snap *Snapshot) (string, error) {
	for range idAttempts {
		suffix := make([]byte, 2)
		if _, err := rand.Read(suffix); err != nil {
			return "", fmt.Errorf("snapshot id: %w", err)
		}
		snap.ID = snap.Created.Format(idLayout) + "-" + hex.EncodeToString(suffix)
		dir := filepath.Join(s.Env.BackupDir(), snap.ID)
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return "", fmt.Errorf("no free snapshot id for %s", snap.Created.Format(idLayout))
}

func (s Store) List() ([]Snapshot, error) {
	dirs, err := os.ReadDir(s.Env.BackupDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", s.Env.BackupDir(), err)
	}
	var snaps []Snapshot
	for _, dir := range dirs {
		if !dir.IsDir() || dir.Name() == blobDir {
			continue
		}
		snap, err := s.load(dir.Name())
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, snap)
	}
	slices.SortFunc(snaps, func(a, b Snapshot) int {
		return cmp.Or(b.Created.Compare(a.Created), cmp.Compare(b.ID, a.ID))
	})
	return snaps, nil
}

func (s Store) load(id string) (Snapshot, error) {
	if id == "" || id == blobDir || filepath.Base(id) != id || !filepath.IsLocal(id) {
		return Snapshot{}, fmt.Errorf("snapshot id %q is not valid", id)
	}
	path := filepath.Join(s.Env.BackupDir(), id, snapshotFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read snapshot %s: %w", id, err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return snap, nil
}

func (s Store) readBlob(sum string) ([]byte, error) {
	content, err := os.ReadFile(s.blobPath(sum))
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w", sum, err)
	}
	if blobSum(content) != sum {
		return nil, fmt.Errorf("blob %s is corrupt", sum)
	}
	return content, nil
}

func (s Store) Prune(keep int) error {
	held, err := fsx.Hold(s.Env.LockPath(), lockWait)
	if err != nil {
		return fmt.Errorf("prune: %w", err)
	}
	defer held.Release()
	return s.PruneHeld(held, keep)
}

func (s Store) PruneHeld(held *fsx.Held, keep int) error {
	if err := held.Check(); err != nil {
		return fmt.Errorf("prune: %w", err)
	}
	snaps, err := s.List()
	if err != nil {
		return err
	}
	referenced := make(map[string]bool)
	rotating := 0
	for _, snap := range snaps {
		if snap.Reason == ReasonChange || snap.Reason == ReasonPreRestore {
			rotating++
			if rotating > keep {
				dir := filepath.Join(s.Env.BackupDir(), snap.ID)
				if err := os.RemoveAll(dir); err != nil {
					return fmt.Errorf("prune %s: %w", dir, err)
				}
				continue
			}
		}
		for _, f := range snap.Files {
			referenced[f.Blob] = true
		}
	}
	return s.pruneBlobs(referenced)
}

func (s Store) pruneBlobs(referenced map[string]bool) error {
	dir := filepath.Join(s.Env.BackupDir(), blobDir)
	blobs, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list %s: %w", dir, err)
	}
	for _, blob := range blobs {
		if referenced[blob.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, blob.Name())); err != nil {
			return fmt.Errorf("prune blob %s: %w", blob.Name(), err)
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
