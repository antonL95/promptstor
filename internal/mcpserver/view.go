package mcpserver

import (
	"fmt"
	"maps"
	"slices"
	"unicode/utf8"

	"github.com/antonL95/promptstor/internal/library"
)

type targetsView struct {
	Include []string `json:"include,omitempty" jsonschema:"tools the item is written to; empty means every selected tool"`
	Exclude []string `json:"exclude,omitempty" jsonschema:"tools the item is never written to"`
}

type entryView struct {
	ID      library.ID  `json:"id"`
	Name    string      `json:"name"`
	Kind    string      `json:"kind"`
	Layer   string      `json:"layer"`
	Targets targetsView `json:"targets"`
}

type fileView struct {
	Mode    string `json:"mode"`
	Size    int    `json:"size"`
	Content string `json:"content,omitempty"`
}

type portableView struct {
	DenyCommands    []string `json:"deny_commands,omitempty"`
	DenyPaths       []string `json:"deny_paths,omitempty"`
	AllowMCPServers []string `json:"allow_mcp_servers,omitempty"`
}

type valueView struct {
	Value string `json:"value,omitempty"`
	Ref   string `json:"ref,omitempty"`
}

type serverView struct {
	Transport string               `json:"transport,omitempty"`
	Command   string               `json:"command,omitempty"`
	URL       string               `json:"url,omitempty"`
	Args      []string             `json:"args,omitempty"`
	Env       map[string]valueView `json:"env,omitempty"`
	Headers   map[string]valueView `json:"headers,omitempty"`
	Resolve   bool                 `json:"resolve,omitempty"`
}

type itemView struct {
	entryView
	Order       int                 `json:"order,omitempty"`
	Description string              `json:"description,omitempty"`
	Scope       []string            `json:"scope,omitempty"`
	Activation  string              `json:"activation,omitempty"`
	Locked      bool                `json:"locked,omitempty"`
	Body        string              `json:"body,omitempty"`
	Meta        map[string]any      `json:"meta,omitempty"`
	Files       map[string]fileView `json:"files,omitempty"`
	Native      map[string]any      `json:"native,omitempty"`
	Portable    *portableView       `json:"portable,omitempty"`
	MCP         *serverView         `json:"mcp,omitempty"`
}

func entryOf(it library.Item) entryView {
	return entryView{
		ID:      it.ID,
		Name:    it.Name,
		Kind:    string(it.Kind),
		Layer:   string(it.Layer),
		Targets: targetsView{Include: it.Targets.Include, Exclude: it.Targets.Exclude},
	}
}

func entriesOf(items []library.Item) []entryView {
	entries := make([]entryView, 0, len(items))
	for _, it := range items {
		entries = append(entries, entryOf(it))
	}
	return entries
}

func itemOf(it library.Item) itemView {
	view := itemView{
		entryView:   entryOf(it),
		Order:       it.Order,
		Description: it.Description,
		Scope:       it.Scope,
		Activation:  string(it.Activation),
		Locked:      it.Locked,
		Body:        it.Body,
		Meta:        it.Meta,
		Native:      it.Native,
	}
	if len(it.Files) > 0 {
		view.Files = map[string]fileView{}
		for _, key := range slices.Sorted(maps.Keys(it.Files)) {
			view.Files[key] = fileOf(it.Files[key])
		}
	}
	if p := it.Portable; p != nil {
		view.Portable = &portableView{DenyCommands: p.DenyCommands, DenyPaths: p.DenyPaths, AllowMCPServers: p.AllowMCPServers}
	}
	if m := it.MCP; m != nil {
		view.MCP = &serverView{
			Transport: m.Transport,
			Command:   m.Command,
			URL:       m.URL,
			Args:      m.Args,
			Env:       valuesOf(m.Env),
			Headers:   valuesOf(m.Headers),
			Resolve:   m.Resolve,
		}
	}
	return view
}

func fileOf(f library.File) fileView {
	view := fileView{Mode: fmt.Sprintf("%04o", f.Mode.Perm()), Size: len(f.Data)}
	if utf8.Valid(f.Data) {
		view.Content = string(f.Data)
	}
	return view
}

func valuesOf(values map[string]library.Value) map[string]valueView {
	if len(values) == 0 {
		return nil
	}
	views := make(map[string]valueView, len(values))
	for name, v := range values {
		views[name] = valueView{Value: v.Literal, Ref: v.Ref}
	}
	return views
}
