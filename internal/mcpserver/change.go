package mcpserver

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/antonL95/promptstor/internal/library"
)

var (
	errOnlyPersonal = errors.New("agents may change only the personal layer")
	errTeamLayer    = errors.New("the company and team layers are available with team accounts")
	errBuiltin      = errors.New("built in items cannot be changed by agents")
)

type itemInput struct {
	ID          string       `json:"id,omitempty" jsonschema:"the id of the item to change; leave it out to create a new item"`
	Kind        string       `json:"kind" jsonschema:"rule, skill, command, subagent, permission, hook or mcp"`
	Name        string       `json:"name" jsonschema:"the item name; skills and commands use lowercase letters, digits and dashes"`
	Description *string      `json:"description,omitempty" jsonschema:"a one line description; left out keeps the current one"`
	Body        *string      `json:"body,omitempty" jsonschema:"the Markdown body; left out keeps the current one"`
	Targets     *targetsView `json:"targets,omitempty" jsonschema:"the tools the item is written to; left out keeps the current ones"`
	Scope       []string     `json:"scope,omitempty" jsonschema:"file globs the item applies to; left out keeps the current ones"`
	Activation  *string      `json:"activation,omitempty" jsonschema:"always, files or agent; left out keeps the current one"`
}

type upsertInput struct {
	Layer      string    `json:"layer" jsonschema:"personal or project"`
	Item       itemInput `json:"item"`
	ProjectDir string    `json:"project_dir,omitempty" jsonschema:"the git repository whose project layer changes; required for the project layer"`
}

type removeInput struct {
	ID         string `json:"id" jsonschema:"the id of the item to remove"`
	Layer      string `json:"layer" jsonschema:"personal or project"`
	ProjectDir string `json:"project_dir,omitempty" jsonschema:"the git repository whose project layer changes; required for the project layer"`
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func checkID(id string) error {
	if id != "" && !safeID.MatchString(id) {
		return fmt.Errorf("id %q may hold only letters, digits, underscores and dashes", id)
	}
	if library.IsBuiltin(library.ID(id)) {
		return errBuiltin
	}
	return nil
}

func checkLayer(layer string) error {
	switch library.Layer(layer) {
	case library.LayerPersonal, library.LayerProject:
		return nil
	case library.LayerDevice:
		return errOnlyPersonal
	case library.LayerCompany, library.LayerTeam:
		return errTeamLayer
	default:
		return fmt.Errorf("unknown layer %q, use personal or project", layer)
	}
}

func (s *server) upsert(in upsertInput) (any, error) {
	if err := checkLayer(in.Layer); err != nil {
		return nil, err
	}
	if err := checkID(in.Item.ID); err != nil {
		return nil, err
	}
	s.write.Lock()
	defer s.write.Unlock()
	if library.Layer(in.Layer) == library.LayerProject {
		return s.upsertProject(in)
	}
	return s.upsertPersonal(in.Item)
}

func (s *server) remove(in removeInput) (any, error) {
	if err := checkLayer(in.Layer); err != nil {
		return nil, err
	}
	if in.ID == "" {
		return nil, errors.New("give the id of the item to remove")
	}
	if err := checkID(in.ID); err != nil {
		return nil, err
	}
	s.write.Lock()
	defer s.write.Unlock()
	if library.Layer(in.Layer) == library.LayerProject {
		return s.removeProject(in)
	}
	return s.removePersonal(in)
}

func build(in itemInput, id library.ID, current *library.Item, layer library.Layer) library.Item {
	it := library.Item{ID: id, Kind: library.Kind(in.Kind), Name: in.Name}
	if current != nil && current.Kind == it.Kind {
		it.Order = current.Order
		it.Description = current.Description
		it.Targets = current.Targets
		it.Scope = current.Scope
		it.Activation = current.Activation
		it.Body = current.Body
		it.Meta = current.Meta
		it.Files = current.Files
		it.Native = current.Native
		it.Portable = current.Portable
		it.MCP = current.MCP
		it.Locked = current.Locked && current.Layer == layer
	}
	if in.Description != nil {
		it.Description = *in.Description
	}
	if in.Body != nil {
		it.Body = *in.Body
	}
	if in.Targets != nil {
		it.Targets = library.Targets{Include: in.Targets.Include, Exclude: in.Targets.Exclude}
	}
	if in.Scope != nil {
		it.Scope = in.Scope
	}
	if in.Activation != nil {
		it.Activation = library.Activation(*in.Activation)
	}
	return it
}

func find(items []library.Item, id library.ID) *library.Item {
	at := slices.IndexFunc(items, func(it library.Item) bool { return it.ID == id })
	if at < 0 {
		return nil
	}
	return &items[at]
}

func store(dir string, it library.Item, current *library.Item) (string, error) {
	if current == nil {
		return library.WriteItem(dir, it)
	}
	path, err := library.ItemPath(dir, it)
	if err != nil {
		return "", err
	}
	if filepath.Clean(path) == filepath.Clean(current.Source) && current.Kind == it.Kind {
		return library.WriteItem(dir, it)
	}
	if err := library.DeleteItem(dir, *current); err != nil {
		return "", err
	}
	written, err := library.WriteItem(dir, it)
	if err != nil {
		if _, restoreErr := library.WriteItem(dir, *current); restoreErr != nil {
			return "", errors.Join(err, fmt.Errorf("restore %s: %w", current.Source, restoreErr))
		}
		return "", err
	}
	return written, nil
}

func tombstone(it library.Item) library.Item {
	return library.Item{ID: it.ID, Kind: it.Kind, Name: it.Name, Removed: true}
}
