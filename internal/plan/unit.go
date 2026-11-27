package plan

import (
	"io/fs"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
)

type Unit struct {
	Tool, Path, Root string
	Kind             manifest.UnitKind
	Key              string
	Content          []byte
	Mode             fs.FileMode
	Items            []library.ID
	Secret           bool
}

type Op string

const (
	OpNoop   Op = "noop"
	OpCreate Op = "create"
	OpUpdate Op = "update"
	OpDelete Op = "delete"
)

type FileChange struct {
	Path, Root    string
	Units         []Unit
	Removed       []manifest.Entry
	Adopted       []Unit
	Exists        bool
	Before, After []byte
	Op            Op
	Blocked       string
	Record        []Unit
	Retagged      []manifest.Entry
	ReplaceLink   bool
	SharedState   bool
	Link          string
	LinkTarget    string
}

type Options struct {
	ReplaceSymlinks, Adopt []string
	SharedState            []string
	External               []string
	Keep                   []manifest.Entry
}

type SnapshotEntry struct {
	Tool, Path, Root string
	Keys, RedactKeys []string
}

type Snapshotter interface {
	Snapshot(reason string, entries []SnapshotEntry) (string, error)
}

type ApplyResult struct {
	SnapshotID string
	Written    []string
	Skipped    []FileChange
}
