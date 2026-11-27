package policy

import (
	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
)

const acknowledgedPrefix = "acknowledged: "

type category struct {
	name        string
	deny        bool
	expressible func(adapter.PortableCaps) bool
	entries     func(*library.PortablePermission) *[]string
}

func categories() []category {
	return []category{
		{
			name:        "deny commands",
			deny:        true,
			expressible: func(c adapter.PortableCaps) bool { return c.DenyCommands },
			entries:     func(p *library.PortablePermission) *[]string { return &p.DenyCommands },
		},
		{
			name:        "deny paths",
			deny:        true,
			expressible: func(c adapter.PortableCaps) bool { return c.DenyPaths },
			entries:     func(p *library.PortablePermission) *[]string { return &p.DenyPaths },
		},
		{
			name:        "allowed MCP servers",
			expressible: func(c adapter.PortableCaps) bool { return c.AllowMCPServers },
			entries:     func(p *library.PortablePermission) *[]string { return &p.AllowMCPServers },
		},
	}
}

func Gate(items []library.Item, tool string, caps adapter.PortableCaps, acks Acks) ([]library.Item, []adapter.Unsupported) {
	var passed []library.Item
	var report []adapter.Unsupported
	for _, item := range items {
		if item.Kind != library.KindPermission || item.Portable == nil {
			passed = append(passed, item)
			continue
		}
		gated, unsupported, blocked := gateItem(item, tool, caps, acks)
		report = append(report, unsupported...)
		if !blocked {
			passed = append(passed, gated)
		}
	}
	return passed, report
}

func gateItem(item library.Item, tool string, caps adapter.PortableCaps, acks Acks) (library.Item, []adapter.Unsupported, bool) {
	acknowledged := acks.Has(item.ID, tool, library.ContentHash(item))
	portable := *item.Portable
	var report []adapter.Unsupported
	blocked := false
	for _, c := range categories() {
		entries := c.entries(&portable)
		if len(*entries) == 0 || c.expressible(caps) {
			continue
		}
		*entries = nil
		unsupported := adapter.Unsupported{ItemID: item.ID, Name: item.Name, Tool: tool, Reason: c.name + " cannot be expressed"}
		switch {
		case c.deny && acknowledged:
			unsupported.Reason = acknowledgedPrefix + unsupported.Reason
		case c.deny:
			unsupported.Blocking = true
			blocked = true
		}
		report = append(report, unsupported)
	}
	if len(report) > 0 {
		item.Portable = &portable
	}
	return item, report, blocked
}
