package drift

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/paths"
)

const lastCheckMode fs.FileMode = 0o600

type LastCheck struct {
	Checked     time.Time           `json:"checked"`
	LibraryHash string              `json:"library_hash"`
	Clean       bool                `json:"clean"`
	Fingerprint string              `json:"fingerprint"`
	Counts      map[FindingKind]int `json:"counts"`
}

func WriteLastCheck(env paths.Env, r Report, now time.Time) error {
	path := env.LastCheckPath()
	data, err := json.MarshalIndent(LastCheck{
		Checked:     now.UTC(),
		LibraryHash: r.LibraryHash,
		Clean:       r.Clean(),
		Fingerprint: r.Fingerprint(),
		Counts:      r.counts(),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := fsx.WriteAtomic(env.StateDir(), path, append(data, '\n'), lastCheckMode); err != nil {
		return fmt.Errorf("write last check: %w", err)
	}
	return nil
}

func ReadLastCheck(env paths.Env) (LastCheck, bool, error) {
	path := env.LastCheckPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return LastCheck{}, false, nil
	}
	if err != nil {
		return LastCheck{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var check LastCheck
	if err := json.Unmarshal(data, &check); err != nil {
		return LastCheck{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	return check, true, nil
}
