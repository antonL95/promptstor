package manifest

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/library"
)

const currentVersion = 1

type UnitKind string

const (
	UnitFile        UnitKind = "file"
	UnitSection     UnitKind = "md-section"
	UnitJSON        UnitKind = "json"
	UnitJSONElement UnitKind = "json-element"
	UnitTOMLTable   UnitKind = "toml-table"
)

type Entry struct {
	Tool    string       `json:"tool"`
	Path    string       `json:"path"`
	Unit    UnitKind     `json:"unit"`
	Key     string       `json:"key"`
	Hash    string       `json:"hash"`
	Items   []library.ID `json:"items,omitempty"`
	Secret  bool         `json:"secret,omitempty"`
	Written time.Time    `json:"written"`
}

type Manifest struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{Version: currentVersion}, nil
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("load manifest %s: %w", path, err)
	}
	var loaded Manifest
	if err := json.Unmarshal(data, &loaded); err != nil {
		return Manifest{}, fmt.Errorf("load manifest %s: %w", path, err)
	}
	if loaded.Version != currentVersion {
		return Manifest{}, fmt.Errorf("load manifest %s: unsupported version %d", path, loaded.Version)
	}
	if len(loaded.Entries) == 0 {
		loaded.Entries = nil
	}
	return loaded, nil
}

func (m *Manifest) Put(e Entry) {
	e.Items = slices.Clone(e.Items)
	if at := m.index(e.Path, e.Unit, e.Key); at >= 0 {
		m.Entries[at] = e
		return
	}
	m.Entries = append(m.Entries, e)
}

func (m *Manifest) Get(path string, u UnitKind, key string) (Entry, bool) {
	at := m.index(path, u, key)
	if at < 0 {
		return Entry{}, false
	}
	return m.Entries[at], true
}

func (m *Manifest) Remove(path string, u UnitKind, key string) {
	if at := m.index(path, u, key); at >= 0 {
		m.Entries = slices.Delete(m.Entries, at, at+1)
	}
}

func (m *Manifest) ForTool(tool string) []Entry {
	return m.matching(func(e Entry) bool { return e.Tool == tool })
}

func (m *Manifest) ForPath(path string) []Entry {
	return m.matching(func(e Entry) bool { return e.Path == path })
}

func (m *Manifest) Save(path string) error {
	sorted := Manifest{Version: currentVersion, Entries: m.matching(func(Entry) bool { return true })}
	if sorted.Entries == nil {
		sorted.Entries = []Entry{}
	}
	data, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return fmt.Errorf("save manifest %s: %w", path, err)
	}
	if err := fsx.WriteAtomic(filepath.Dir(path), path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("save manifest %s: %w", path, err)
	}
	return nil
}

func (m *Manifest) index(path string, u UnitKind, key string) int {
	return slices.IndexFunc(m.Entries, func(e Entry) bool {
		return e.Path == path && e.Unit == u && e.Key == key
	})
}

func (m *Manifest) matching(keep func(Entry) bool) []Entry {
	var matched []Entry
	for _, e := range m.Entries {
		if keep(e) {
			matched = append(matched, e)
		}
	}
	slices.SortFunc(matched, compareEntries)
	return matched
}

func compareEntries(a, b Entry) int {
	return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Unit, b.Unit), cmp.Compare(a.Key, b.Key))
}
