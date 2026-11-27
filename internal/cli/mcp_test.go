package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/library"
)

type rpcResponse struct {
	ID     int `json:"id"`
	Result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	} `json:"result"`
}

func TestMCPServesOnStdioAndExitsWhenStdinCloses(t *testing.T) {
	env := cliHome(t)
	d := deps(env)
	stdin, send := io.Pipe()
	receive, stdout := io.Pipe()
	d.Stdin = stdin
	d.Stdout = stdout
	cmd := NewRoot(d)
	cmd.SetArgs([]string{"mcp"})
	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
		_ = stdout.Close()
	}()
	responses := bufio.NewScanner(receive)
	exchange := func(request string) rpcResponse {
		t.Helper()
		if _, err := io.WriteString(send, request+"\n"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(request, `"id"`) {
			return rpcResponse{}
		}
		if !responses.Scan() {
			t.Fatalf("no response to %s: %v", request, responses.Err())
		}
		var response rpcResponse
		if err := json.Unmarshal(responses.Bytes(), &response); err != nil {
			t.Fatalf("response %q: %v", responses.Text(), err)
		}
		return response
	}

	exchange(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fixture-client","version":"1"}}}`)
	exchange(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	listed := exchange(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if err := send.Close(); err != nil {
		t.Fatal(err)
	}

	var tools []string
	for _, tool := range listed.Result.Tools {
		tools = append(tools, tool.Name)
	}
	want := "promptstor_get promptstor_list promptstor_remove promptstor_search promptstor_status promptstor_upsert"
	if got := strings.Join(tools, " "); listed.ID != 2 || got != want {
		t.Errorf("response %d tools %q", listed.ID, got)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("mcp exited with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mcp did not exit after stdin closed")
	}
}

func TestEngineRegistersMCPWhenEnabled(t *testing.T) {
	for name, tc := range map[string]struct {
		register bool
		binary   string
		want     bool
	}{
		"enabled":   {register: true, binary: guardBinary, want: true},
		"disabled":  {register: false, binary: guardBinary},
		"no binary": {register: true},
	} {
		t.Run(name, func(t *testing.T) {
			env := cliHome(t)
			cfg := config.Config{Version: 1, Tools: []string{"claude-code"}, RegisterMCP: tc.register}
			if err := cfg.Save(env.ConfigFile()); err != nil {
				t.Fatal(err)
			}
			d := deps(env)
			d.Binary = tc.binary
			e, err := d.engine()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, it := range e.Builtins {
				if it.ID == library.PromptstorMCP(guardBinary).ID {
					found = true
					if it.MCP == nil || it.MCP.Command != guardBinary {
						t.Errorf("server %+v", it.MCP)
					}
				}
			}
			if found != tc.want {
				t.Errorf("registered %v, want %v", found, tc.want)
			}
		})
	}
}
