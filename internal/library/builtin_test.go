package library

import (
	"slices"
	"testing"
)

func TestPromptstorMCPItem(t *testing.T) {
	it := PromptstorMCP("/opt/prompt stor/bin/promptstor")

	if it.ID != "ps_builtin_mcp" || it.Name != "promptstor" || it.Kind != KindMCP || it.Layer != LayerCompany {
		t.Fatalf("item %+v", it)
	}
	if it.MCP == nil || it.MCP.Transport != "stdio" || it.MCP.Command != "/opt/prompt stor/bin/promptstor" || !slices.Equal(it.MCP.Args, []string{"mcp"}) {
		t.Errorf("server %+v", it.MCP)
	}
	if IsEditGuard(it) || !IsBuiltin(it.ID) {
		t.Error("the MCP item is classified wrongly")
	}
}

func TestIsBuiltin(t *testing.T) {
	for id, want := range map[ID]bool{
		EditGuard("claude-code", "promptstor").ID: true,
		OpencodeGuard(nil).ID:                     true,
		"ps_builtin_mcp":                          true,
		"ps_0000000000000000000000MCP1":           false,
	} {
		if IsBuiltin(id) != want {
			t.Errorf("IsBuiltin(%s) = %v", id, !want)
		}
	}
}
