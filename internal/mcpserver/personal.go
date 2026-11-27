package mcpserver

import (
	"fmt"
	"path/filepath"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/plan"
)

func (s *server) personalDir() (string, []library.Item, error) {
	dir, err := filepath.Abs(s.e.Env.LibraryDir())
	if err != nil {
		return "", nil, fmt.Errorf("resolve %s: %w", s.e.Env.LibraryDir(), err)
	}
	items, err := library.LoadLayer(dir, library.LayerPersonal, library.LoadOptions{})
	return dir, items, err
}

func (s *server) upsertPersonal(in itemInput) (any, error) {
	dir, personal, err := s.personalDir()
	if err != nil {
		return nil, err
	}
	id := library.ID(in.ID)
	current := find(personal, id)
	base := current
	if id != "" && current == nil {
		merged, _, err := s.e.Library("")
		if err != nil {
			return nil, err
		}
		inherited := find(merged.Items, id)
		switch {
		case inherited == nil:
			return nil, fmt.Errorf("no item with id %s; leave the id out to create a new item", id)
		case inherited.Layer == library.LayerDevice:
			return nil, fmt.Errorf("the device layer holds %s: %w", id, errOnlyPersonal)
		}
		base = inherited
	}
	if id == "" {
		id = library.NewID()
	}
	it := build(in, id, base, library.LayerPersonal)
	path, err := store(dir, it, current)
	if err != nil {
		return nil, err
	}
	return s.syncUser(changeView{Layer: string(library.LayerPersonal), ID: id, Name: it.Name, Path: path})
}

func (s *server) removePersonal(in removeInput) (any, error) {
	dir, personal, err := s.personalDir()
	if err != nil {
		return nil, err
	}
	root, err := projectRoot(in.ProjectDir)
	if err != nil {
		return nil, err
	}
	id := library.ID(in.ID)
	before, _, err := s.e.Library(root)
	if err != nil {
		return nil, err
	}
	if shown := find(before.Items, id); shown != nil && shown.Layer == library.LayerDevice {
		return nil, fmt.Errorf("the device layer holds %s: %w", id, errOnlyPersonal)
	}
	view := changeView{Layer: string(library.LayerPersonal), ID: id}
	if current := find(personal, id); current != nil {
		if current.Removed {
			return nil, fmt.Errorf("%s is already removed", id)
		}
		if err := library.DeleteItem(dir, *current); err != nil {
			return nil, err
		}
		view.Name, view.Path = current.Name, current.Source
	}
	merged, _, err := s.e.Library(root)
	if err != nil {
		return nil, err
	}
	inherited := find(merged.Items, id)
	switch {
	case inherited == nil && view.Name == "":
		return nil, fmt.Errorf("no item with id %s", id)
	case inherited != nil:
		path, err := library.WriteItem(dir, tombstone(*inherited))
		if err != nil {
			return nil, err
		}
		view.Name, view.Path = inherited.Name, path
	}
	return s.syncUser(view)
}

func (s *server) syncUser(view changeView) (any, error) {
	result, p, err := s.e.Sync("", plan.Options{})
	if err != nil {
		return nil, fmt.Errorf("saved %s in the personal layer, but the sync failed: %w", view.Name, err)
	}
	view.Changed = result.Written
	view.Snapshot = result.SnapshotID
	view.report(p, "")
	return view, nil
}
