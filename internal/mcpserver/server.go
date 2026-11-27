package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/antonL95/promptstor/internal/engine"
)

const (
	serverName    = "promptstor"
	serverVersion = "1"
)

type runner func(dir, name string, args ...string) ([]byte, error)

type server struct {
	e     engine.Engine
	run   runner
	write sync.Mutex
}

func New(e engine.Engine, run func(dir, name string, args ...string) ([]byte, error)) *mcp.Server {
	s := &server{e: e, run: run}
	srv := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "promptstor_list",
		Description: "List the merged library items as id, name, kind, layer and targets. Filter by layer, kind or tool.",
	}, handle(s.list))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "promptstor_get",
		Description: "Return one library item in full, found by id or by name.",
	}, handle(s.get))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "promptstor_search",
		Description: "Find library items whose name, description or body contains the query, ignoring case.",
	}, handle(s.search))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "promptstor_upsert",
		Description: "Create or change a library item. The personal layer is written and synced at once. The project layer becomes a git branch in the repository at project_dir.",
	}, handle(s.upsert))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "promptstor_remove",
		Description: "Remove a library item. On the personal layer an item inherited from another layer is hidden with a tombstone. The project layer becomes a git branch.",
	}, handle(s.remove))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "promptstor_status",
		Description: "Return the drift report as JSON, for the user scope and optionally the project at project_dir.",
	}, handle(s.status))
	return srv
}

func handle[In any](f func(In) (any, error)) mcp.ToolHandlerFor[In, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		out, err := f(in)
		if err != nil {
			return nil, nil, err
		}
		text, isRaw := out.(rawJSON)
		if !isRaw {
			data, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				return nil, nil, fmt.Errorf("encode result: %w", err)
			}
			text = rawJSON(data)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}, nil, nil
	}
}

type rawJSON string

func (s *server) now() time.Time {
	if s.e.Now == nil {
		return time.Now()
	}
	return s.e.Now()
}
