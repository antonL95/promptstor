package doctor

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/plan"
)

var errJSONNeedsYes = errors.New("doctor --json needs --yes or --dry-run")

type firstRunJSON struct {
	Tools       []toolJSON        `json:"tools"`
	Comparison  []drift.Finding   `json:"comparison"`
	Planned     []plannedJSON     `json:"planned"`
	Blocked     []blockedJSON     `json:"blocked"`
	Unsupported []unsupportedJSON `json:"unsupported"`
}

type toolJSON struct {
	Tool      string   `json:"tool"`
	Installed bool     `json:"installed"`
	Paths     []string `json:"paths"`
}

type plannedJSON struct {
	Path string  `json:"path"`
	Op   plan.Op `json:"op"`
}

type blockedJSON struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type unsupportedJSON struct {
	Tool     string     `json:"tool"`
	Item     library.ID `json:"item,omitempty"`
	Name     string     `json:"name,omitempty"`
	Reason   string     `json:"reason"`
	Blocking bool       `json:"blocking"`
}

func (d *doctor) firstRunJSON() (int, error) {
	if !d.opt.Yes && !d.opt.DryRun {
		return 2, errJSONNeedsYes
	}
	out := d.w.w
	d.w = &writer{w: io.Discard}
	s := &firstRunState{}
	code, err := d.firstRun(s)
	if err != nil {
		return code, err
	}
	data, err := json.MarshalIndent(s.document(), "", "  ")
	if err != nil {
		return 2, fmt.Errorf("encode doctor report: %w", err)
	}
	if _, err := out.Write(append(data, '\n')); err != nil {
		return 2, fmt.Errorf("write doctor report: %w", err)
	}
	return code, nil
}

func (s *firstRunState) document() firstRunJSON {
	doc := firstRunJSON{
		Tools:       []toolJSON{},
		Comparison:  append([]drift.Finding{}, s.comparison...),
		Planned:     []plannedJSON{},
		Blocked:     []blockedJSON{},
		Unsupported: []unsupportedJSON{},
	}
	for _, t := range s.found {
		doc.Tools = append(doc.Tools, toolJSON{Tool: t.a.ID(), Installed: t.installed, Paths: append([]string{}, t.paths...)})
	}
	for _, c := range s.plan.Changes {
		switch {
		case c.Blocked != "":
			doc.Blocked = append(doc.Blocked, blockedJSON{Path: c.Path, Reason: c.Blocked})
		case c.Op != plan.OpNoop:
			doc.Planned = append(doc.Planned, plannedJSON{Path: c.Path, Op: c.Op})
		}
	}
	for _, u := range sortedUnsupported(s.plan.Unsupported) {
		doc.Unsupported = append(doc.Unsupported, unsupportedJSON{Tool: u.Tool, Item: u.ItemID, Name: u.Name, Reason: u.Reason, Blocking: u.Blocking})
	}
	return doc
}

func sortedUnsupported(report []adapter.Unsupported) []adapter.Unsupported {
	sorted := slices.Clone(report)
	slices.SortStableFunc(sorted, func(a, b adapter.Unsupported) int {
		return cmp.Or(cmp.Compare(a.Tool, b.Tool), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Reason, b.Reason))
	})
	return sorted
}
