package mcpserver

import (
	"path/filepath"

	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/library"
)

type blockedView struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type unsupportedView struct {
	Tool     string     `json:"tool"`
	ItemID   library.ID `json:"item_id,omitempty"`
	Name     string     `json:"name,omitempty"`
	Reason   string     `json:"reason"`
	Blocking bool       `json:"blocking,omitempty"`
}

type violationView struct {
	ItemID library.ID `json:"item_id"`
	Name   string     `json:"name"`
	Layer  string     `json:"layer"`
	Reason string     `json:"reason"`
}

type changeView struct {
	Layer       string            `json:"layer"`
	ID          library.ID        `json:"id"`
	Name        string            `json:"name"`
	Path        string            `json:"path,omitempty"`
	Changed     []string          `json:"changed"`
	Snapshot    string            `json:"snapshot,omitempty"`
	Branch      string            `json:"branch,omitempty"`
	PullRequest string            `json:"pull_request,omitempty"`
	Blocked     []blockedView     `json:"blocked,omitempty"`
	Unsupported []unsupportedView `json:"unsupported,omitempty"`
	Violations  []violationView   `json:"violations,omitempty"`
	Warnings    []string          `json:"warnings,omitempty"`
}

func (v *changeView) report(p engine.Plan, root string) {
	for _, c := range p.Changes {
		if c.Blocked != "" {
			v.Blocked = append(v.Blocked, blockedView{Path: relative(root, c.Path), Reason: c.Blocked})
		}
	}
	for _, u := range p.Unsupported {
		v.Unsupported = append(v.Unsupported, unsupportedView{Tool: u.Tool, ItemID: u.ItemID, Name: u.Name, Reason: u.Reason, Blocking: u.Blocking})
	}
	for _, violation := range p.Violations {
		v.Violations = append(v.Violations, violationView{ItemID: violation.ItemID, Name: violation.Name, Layer: string(violation.Layer), Reason: violation.Reason})
	}
	if v.Changed == nil {
		v.Changed = []string{}
	}
}

func relative(root, path string) string {
	if root == "" {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
