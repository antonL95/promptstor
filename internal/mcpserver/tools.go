package mcpserver

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/library"
)

type listInput struct {
	Layer string `json:"layer,omitempty" jsonschema:"only items from this layer: company, team, project, personal or device"`
	Kind  string `json:"kind,omitempty" jsonschema:"only items of this kind: rule, skill, command, subagent, permission, hook or mcp"`
	Tool  string `json:"tool,omitempty" jsonschema:"only items written to this tool, such as claude-code or codex"`
}

type getInput struct {
	ID   string `json:"id,omitempty" jsonschema:"the item id"`
	Name string `json:"name,omitempty" jsonschema:"the item name, used when no id is given"`
}

type searchInput struct {
	Query string `json:"query" jsonschema:"text to find in the name, description or body"`
}

type statusInput struct {
	ProjectDir string `json:"project_dir,omitempty" jsonschema:"also check the project in this directory"`
}

func (s *server) items() ([]library.Item, error) {
	merged, _, err := s.e.Library("")
	if err != nil {
		return nil, err
	}
	return merged.Items, nil
}

func (s *server) list(in listInput) (any, error) {
	items, err := s.items()
	if err != nil {
		return nil, err
	}
	if in.Tool != "" {
		items = library.Merged{Items: items}.ForTool(in.Tool)
	}
	items = slices.DeleteFunc(items, func(it library.Item) bool {
		return (in.Layer != "" && string(it.Layer) != in.Layer) || (in.Kind != "" && string(it.Kind) != in.Kind)
	})
	return entriesOf(items), nil
}

func (s *server) get(in getInput) (any, error) {
	items, err := s.items()
	if err != nil {
		return nil, err
	}
	switch {
	case in.ID != "":
		at := slices.IndexFunc(items, func(it library.Item) bool { return string(it.ID) == in.ID })
		if at < 0 {
			return nil, fmt.Errorf("no item with id %s", in.ID)
		}
		return itemOf(items[at]), nil
	case in.Name != "":
		var found []library.Item
		for _, it := range items {
			if it.Name == in.Name {
				found = append(found, it)
			}
		}
		switch len(found) {
		case 0:
			return nil, fmt.Errorf("no item named %q", in.Name)
		case 1:
			return itemOf(found[0]), nil
		default:
			var ids []string
			for _, it := range found {
				ids = append(ids, string(it.ID))
			}
			return nil, fmt.Errorf("several items are named %q, ask by id: %s", in.Name, strings.Join(ids, ", "))
		}
	default:
		return nil, errors.New("give an id or a name")
	}
}

func (s *server) search(in searchInput) (any, error) {
	query := strings.ToLower(strings.TrimSpace(in.Query))
	if query == "" {
		return nil, errors.New("the query is empty")
	}
	items, err := s.items()
	if err != nil {
		return nil, err
	}
	items = slices.DeleteFunc(items, func(it library.Item) bool {
		return !slices.ContainsFunc([]string{it.Name, it.Description, it.Body}, func(field string) bool {
			return strings.Contains(strings.ToLower(field), query)
		})
	})
	return entriesOf(items), nil
}

func (s *server) status(in statusInput) (any, error) {
	root, err := projectRoot(in.ProjectDir)
	if err != nil {
		return nil, err
	}
	report, err := drift.Check(s.e, root)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := drift.RenderJSON(&out, report); err != nil {
		return nil, err
	}
	return rawJSON(out.String()), nil
}

func projectRoot(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	return abs, nil
}
