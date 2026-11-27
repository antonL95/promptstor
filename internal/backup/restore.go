package backup

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

	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/structedit"
)

const (
	OpWrite   = "write"
	OpLink    = "link"
	OpDelete  = "delete"
	OpSetKeys = "set-keys"
	OpPrune   = "prune"
	OpBlocked = "blocked"
)

type RestoreAction struct {
	Path   string
	Op     string
	Detail string
}

type RestorePlan struct {
	Snapshot     Snapshot
	Actions      []RestoreAction
	PreRestoreID string
	Unresolved   []string
}

func (s Store) Restore(id, tool string, dryRun bool) (RestorePlan, error) {
	snap, err := s.load(id)
	if err != nil {
		return RestorePlan{}, err
	}
	r := restorer{store: s, dryRun: dryRun, plan: RestorePlan{Snapshot: snap}}
	if !dryRun {
		release, err := fsx.Acquire(s.Env.LockPath(), lockWait)
		if err != nil {
			return r.plan, fmt.Errorf("restore: %w", err)
		}
		defer release()
		pre, err := s.Take(ReasonPreRestore, entriesFor(snap.Entries, tool))
		if err != nil {
			return r.plan, fmt.Errorf("snapshot before restoring: %w", err)
		}
		r.plan.PreRestoreID = pre.ID
	}
	files := filesFor(snap.Files, tool)
	if err := r.pruneAll(files, snap.Files); err != nil {
		return r.plan, err
	}
	for _, f := range files {
		if err := r.restore(f); err != nil {
			return r.plan, fmt.Errorf("restore %s: %w", f.Path, err)
		}
	}
	slices.Sort(r.plan.Unresolved)
	return r.plan, nil
}

func belongsTo(tools, tool string) bool {
	return tool == "" || slices.Contains(strings.Split(tools, ","), tool)
}

func entriesFor(entries []plan.SnapshotEntry, tool string) []plan.SnapshotEntry {
	var kept []plan.SnapshotEntry
	for _, entry := range entries {
		if belongsTo(entry.Tool, tool) {
			kept = append(kept, entry)
		}
	}
	return kept
}

func filesFor(files []SnapFile, tool string) []SnapFile {
	var kept []SnapFile
	for _, f := range files {
		if belongsTo(f.Tool, tool) {
			kept = append(kept, f)
		}
	}
	slices.SortStableFunc(kept, func(a, b SnapFile) int { return cmp.Compare(a.Path, b.Path) })
	return kept
}

type restorer struct {
	store  Store
	dryRun bool
	plan   RestorePlan
}

func (r *restorer) act(path, op, detail string) {
	r.plan.Actions = append(r.plan.Actions, RestoreAction{Path: path, Op: op, Detail: detail})
}

func (r *restorer) rootOf(f SnapFile) string {
	return cmp.Or(f.Root, r.store.Env.Home)
}

func (r *restorer) blocked(f SnapFile) (bool, error) {
	path := f.Path
	if Excluded(r.store.Env, path) {
		return true, nil
	}
	link, linked, err := fsx.LinkedAncestor(r.rootOf(f), filepath.Dir(path))
	if err != nil {
		r.act(path, OpBlocked, err.Error())
		return true, nil
	}
	if linked {
		r.act(path, OpBlocked, "parent "+link+" is a symlink")
		return true, nil
	}
	return false, nil
}

func (r *restorer) restore(f SnapFile) error {
	if stop, err := r.blocked(f); stop || err != nil {
		return err
	}
	current, err := fsx.Inspect(f.Path)
	if err != nil {
		return err
	}
	if f.Keys != nil {
		return r.restoreKeys(f, current)
	}
	switch f.Kind {
	case KindAbsent:
		return r.restoreAbsent(f, current)
	case KindSymlink:
		return r.restoreLink(f, current)
	case KindDir:
		return r.restoreDir(f, current)
	case KindFile:
		return r.restoreFile(f, current)
	default:
		r.act(f.Path, OpBlocked, fmt.Sprintf("unknown kind %q", f.Kind))
		return nil
	}
}

func (r *restorer) restoreAbsent(f SnapFile, current fsx.Entry) error {
	if !current.Exists {
		return nil
	}
	if ok, err := r.clearable(f.Path, current); !ok || err != nil {
		return err
	}
	r.act(f.Path, OpDelete, "")
	return r.remove(f.Path)
}

func (r *restorer) restoreLink(f SnapFile, current fsx.Entry) error {
	if current.IsLink && current.LinkTarget == f.LinkTarget {
		return nil
	}
	if ok, err := r.clearable(f.Path, current); !ok || err != nil {
		return err
	}
	r.act(f.Path, OpLink, f.LinkTarget)
	if r.dryRun {
		return nil
	}
	if err := r.clear(f.Path, current); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	return os.Symlink(f.LinkTarget, f.Path)
}

func (r *restorer) restoreDir(f SnapFile, current fsx.Entry) error {
	perm := f.Mode.Perm()
	if current.IsDir && !current.IsLink {
		if current.Mode.Perm() == perm {
			return nil
		}
		r.act(f.Path, OpWrite, fmt.Sprintf("directory mode %v", perm))
		if r.dryRun {
			return nil
		}
		return os.Chmod(f.Path, perm)
	}
	r.act(f.Path, OpWrite, "directory")
	if r.dryRun {
		return nil
	}
	if err := r.clear(f.Path, current); err != nil {
		return err
	}
	if err := os.MkdirAll(f.Path, perm); err != nil {
		return err
	}
	return os.Chmod(f.Path, perm)
}

func (r *restorer) restoreFile(f SnapFile, current fsx.Entry) error {
	stored, err := r.store.readBlob(f.Blob)
	if err != nil {
		r.act(f.Path, OpBlocked, err.Error())
		return nil
	}
	existing, err := r.regularContent(f.Path, current)
	if err != nil {
		return err
	}
	excluded := ExcludedPointers(r.store.Env, f.Path)
	if len(excluded) > 0 && current.Exists && !isRegular(current) {
		r.act(f.Path, OpBlocked, "replacing it would drop credentials kept in the current file")
		return nil
	}
	content, err := carryOver(stored, existing, excluded)
	if err != nil {
		r.act(f.Path, OpBlocked, err.Error())
		return nil
	}
	if content, err = r.resolveSecrets(f, content, existing); err != nil {
		return err
	}
	perm := f.Mode.Perm()
	if isRegular(current) && current.Mode.Perm() == perm && bytes.Equal(content, existing) {
		return nil
	}
	if ok, err := r.clearable(f.Path, current); !ok || err != nil {
		return err
	}
	r.act(f.Path, OpWrite, fmt.Sprintf("mode %v", perm))
	return r.write(f, current, content, perm)
}

func (r *restorer) restoreKeys(f SnapFile, current fsx.Entry) error {
	if current.Exists && !isRegular(current) {
		r.act(f.Path, OpBlocked, "holds keys but is not a regular file")
		return nil
	}
	existing, err := r.regularContent(f.Path, current)
	if err != nil {
		return err
	}
	content := existing
	keys := sortedKeys(f.Keys)
	for _, key := range keys {
		if value := f.Keys[key]; value != "" {
			content, err = structedit.JSONSet(content, key, []byte(value))
		} else {
			content, err = structedit.JSONDelete(content, key)
		}
		if err != nil {
			r.act(f.Path, OpBlocked, err.Error())
			return nil
		}
	}
	if content, err = r.resolveSecrets(f, content, existing); err != nil {
		return err
	}
	if bytes.Equal(content, existing) {
		return nil
	}
	perm := current.Mode.Perm()
	if !current.Exists {
		perm = cmp.Or(f.Mode.Perm(), 0o600)
	}
	r.act(f.Path, OpSetKeys, strings.Join(keys, ", "))
	return r.write(f, current, content, perm)
}

func isRegular(e fsx.Entry) bool {
	return e.Exists && !e.IsLink && e.Mode.IsRegular()
}

func (r *restorer) regularContent(path string, current fsx.Entry) ([]byte, error) {
	if !isRegular(current) {
		return nil, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return content, nil
}

func (r *restorer) resolveSecrets(f SnapFile, content, existing []byte) ([]byte, error) {
	for _, key := range f.Redacted {
		value, err := r.resolve(f, key)
		if err != nil {
			r.plan.Unresolved = append(r.plan.Unresolved, f.Path+" "+key)
			value, err = keptValue(existing, key)
			if err != nil {
				return nil, fmt.Errorf("keep %s %s: %w", f.Path, key, err)
			}
		}
		if value == nil {
			content, err = structedit.JSONDelete(content, key)
		} else {
			content, err = structedit.JSONSet(content, key, value)
		}
		if err != nil {
			return nil, fmt.Errorf("restore secret %s: %w", key, err)
		}
	}
	return content, nil
}

func (r *restorer) resolve(f SnapFile, key string) ([]byte, error) {
	if r.store.Resolve == nil {
		return nil, errors.New("no secret resolver")
	}
	return r.store.Resolve(f.Tool, f.Path, key)
}

func keptValue(existing []byte, key string) ([]byte, error) {
	value, found, err := structedit.JSONGet(existing, key)
	if err != nil || !found {
		return nil, err
	}
	return value, nil
}

func (r *restorer) clearable(path string, current fsx.Entry) (bool, error) {
	if !current.IsDir || current.IsLink {
		return true, nil
	}
	children, err := os.ReadDir(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if len(children) > 0 {
		r.act(path, OpBlocked, "a directory that is not empty is in the way")
		return false, nil
	}
	return true, nil
}

func (r *restorer) clear(path string, current fsx.Entry) error {
	if !current.Exists {
		return nil
	}
	return os.Remove(path)
}

func (r *restorer) remove(path string) error {
	if r.dryRun {
		return nil
	}
	return os.Remove(path)
}

func (r *restorer) write(f SnapFile, current fsx.Entry, content []byte, perm fs.FileMode) error {
	path := f.Path
	if r.dryRun {
		return nil
	}
	if current.Exists && !isRegular(current) {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	if err := fsx.WriteAtomic(r.rootOf(f), path, content, perm); err != nil {
		return err
	}
	return os.Chmod(path, perm)
}
