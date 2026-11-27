package engine

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/fsx"
	"github.com/antonL95/promptstor/internal/library"
)

const (
	libraryTool  = "library"
	reasonLinked = "reached through the symlink %s, so ids are not written into its items"
)

type layerSource struct {
	dir, root string
	layer     library.Layer
}

func (e Engine) layerSources(projectRoot string) []layerSource {
	personal, device := e.Env.LibraryDir(), e.Env.DeviceDir()
	sources := []layerSource{
		{dir: personal, root: e.rootOf(personal), layer: library.LayerPersonal},
		{dir: device, root: e.rootOf(device), layer: library.LayerDevice},
	}
	if projectRoot != "" {
		sources = append(sources, layerSource{dir: filepath.Join(projectRoot, projectLayerDir), root: projectRoot, layer: library.LayerProject})
	}
	return sources
}

func (e Engine) rootOf(dir string) string {
	if relative, err := filepath.Rel(e.Env.Home, dir); err == nil && filepath.IsLocal(relative) {
		return e.Env.Home
	}
	return e.Env.ConfigHome
}

func (e Engine) WriteMissingIDs(projectRoot string) (map[library.ID]library.ID, []adapter.Unsupported, error) {
	missing, err := e.hasMissingIDs(projectRoot)
	if err != nil || !missing {
		return nil, nil, err
	}
	release, err := fsx.Acquire(e.Env.LockPath(), lockWait)
	if err != nil {
		return nil, nil, fmt.Errorf("write missing ids: %w", err)
	}
	defer release()
	renamed := map[library.ID]library.ID{}
	var notes []adapter.Unsupported
	for _, source := range e.layerSources(projectRoot) {
		assigned, err := library.WriteMissingIDs(source.root, source.dir, source.layer)
		if err != nil {
			return nil, nil, fmt.Errorf("write ids into the %s layer: %w", source.layer, err)
		}
		maps.Copy(renamed, assigned.Renamed)
		if assigned.Linked != "" {
			notes = append(notes, adapter.Unsupported{Tool: libraryTool, Name: source.dir, Reason: fmt.Sprintf(reasonLinked, assigned.Linked)})
		}
	}
	return renamed, notes, nil
}

func (e Engine) hasMissingIDs(projectRoot string) (bool, error) {
	layers, err := e.layers(projectRoot)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(layers, func(l library.LayerItems) bool {
		return slices.ContainsFunc(l.Items, func(it library.Item) bool { return library.IsTemporary(it.ID) })
	}), nil
}
