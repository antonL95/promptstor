package engine_test

import (
	"path/filepath"
	"testing"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/plan"
)

const kept = 100

func fillSnapshots(t *testing.T, e engine.Engine, count int) {
	t.Helper()
	for range count {
		if _, err := e.Backups().Take(backup.ReasonChange, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func snapshotCount(t *testing.T, e engine.Engine) int {
	t.Helper()
	snaps, err := e.Backups().List()
	if err != nil {
		t.Fatal(err)
	}
	return len(snaps)
}

func TestSyncPrunesOldSnapshots(t *testing.T) {
	e := newEngine(home(t), adapter.ClaudeCode)
	fillSnapshots(t, e, kept+5)

	if _, err := e.Plan("", plan.Options{}); err != nil {
		t.Fatal(err)
	}
	if got := snapshotCount(t, e); got != kept+5 {
		t.Fatalf("a dry run left %d snapshots", got)
	}
	if _, _, err := e.Sync("", plan.Options{}); err != nil {
		t.Fatal(err)
	}

	if got := snapshotCount(t, e); got != kept {
		t.Errorf("%d snapshots after sync, want %d", got, kept)
	}
}

func TestRestorePrunesOldSnapshots(t *testing.T) {
	e := newEngine(home(t), adapter.ClaudeCode)
	result, _, err := e.Sync("", plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	fillSnapshots(t, e, kept+5)
	snaps, err := e.Backups().List()
	if err != nil {
		t.Fatal(err)
	}
	newest := snaps[0].ID
	before := len(snaps)

	if _, err := e.Restore(newest, "", true); err != nil {
		t.Fatal(err)
	}
	if got := snapshotCount(t, e); got != before {
		t.Fatalf("a dry run restore left %d of %d snapshots", got, before)
	}
	if _, err := e.Restore(newest, "", false); err != nil {
		t.Fatalf("restore after %s: %v", result.SnapshotID, err)
	}

	if got := snapshotCount(t, e); got != kept {
		t.Errorf("%d snapshots after restore, want %d", got, kept)
	}
}

func TestPruneFailureIsReportedAndIgnored(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode)
	var warnings []error
	e.Warn = func(err error) { warnings = append(warnings, err) }
	writeFile(t, filepath.Join(env.BackupDir(), "broken", "snapshot.json"), "not json")

	result, _, err := e.Sync("", plan.Options{})

	if err != nil || len(result.Written) == 0 {
		t.Fatalf("sync failed with the prune: %v %+v", err, result)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings %v", warnings)
	}
}
