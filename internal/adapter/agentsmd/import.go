package agentsmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/mdrules"
)

var errNotRegular = errors.New("not a regular file")

func (agentsMD) Import(t adapter.Target) (adapter.Imported, error) {
	path, ok := projectFile(t)
	if !ok {
		return adapter.Imported{}, nil
	}
	entry, err := fsx.Inspect(path)
	switch {
	case err != nil || !entry.Exists:
		return adapter.Imported{}, err
	case entry.IsLink:
		note := adapter.Note{Path: path, Message: "symlinked -> " + entry.LinkTarget + "; not imported"}
		return adapter.Imported{Notes: []adapter.Note{note}}, nil
	case !entry.Mode.IsRegular():
		return adapter.Imported{}, fmt.Errorf("read %s: %w", path, errNotRegular)
	}
	document, err := os.ReadFile(path)
	if err != nil {
		return adapter.Imported{}, fmt.Errorf("read %s: %w", path, err)
	}
	return adapter.Imported{Items: mdrules.Split(document, path)}, nil
}
