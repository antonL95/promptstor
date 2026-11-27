package policy

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

const acksMode fs.FileMode = 0o600

type Ack struct {
	ItemID library.ID `json:"item_id"`
	Tool   string     `json:"tool"`
	Hash   string     `json:"hash"`
	At     time.Time  `json:"at"`
}

type Acks struct {
	List []Ack `json:"acks"`
}

func LoadAcks(path string) (Acks, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Acks{}, nil
	}
	if err != nil {
		return Acks{}, fmt.Errorf("load acks %s: %w", path, err)
	}
	var acks Acks
	if err := json.Unmarshal(data, &acks); err != nil {
		return Acks{}, fmt.Errorf("load acks %s: %w", path, err)
	}
	return acks, nil
}

func (a *Acks) Add(x Ack) {
	a.List = slices.DeleteFunc(a.List, func(old Ack) bool { return old.ItemID == x.ItemID && old.Tool == x.Tool })
	a.List = append(a.List, x)
}

func (a Acks) Has(id library.ID, tool, hash string) bool {
	return hash != "" && slices.ContainsFunc(a.List, func(x Ack) bool {
		return x.ItemID == id && x.Tool == tool && x.Hash == hash
	})
}

func (a Acks) Save(path string) error {
	sorted := Acks{List: append([]Ack{}, a.List...)}
	slices.SortFunc(sorted.List, func(x, y Ack) int {
		return cmp.Or(cmp.Compare(x.ItemID, y.ItemID), cmp.Compare(x.Tool, y.Tool))
	})
	data, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return fmt.Errorf("save acks %s: %w", path, err)
	}
	if err := fsx.WriteAtomic(filepath.Dir(path), path, append(data, '\n'), acksMode); err != nil {
		return fmt.Errorf("save acks %s: %w", path, err)
	}
	return nil
}
