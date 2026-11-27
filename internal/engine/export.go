package engine

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/policy"
	"github.com/antonL95/promptstor/internal/secrets"
)

const (
	acknowledgedPrefix = "acknowledged: "
	ReasonNeedsSetup   = "needs manual setup: "
	reasonUnresolved   = "secret not resolved: "
)

type exported struct {
	units       []plan.Unit
	unsupported []adapter.Unsupported
	keep        []manifest.Entry
}

type toolExport struct {
	tool        string
	originals   map[library.ID]library.Item
	unsupported []adapter.Unsupported
	held        map[library.ID]bool
	secret      map[library.ID]bool
}

func (e Engine) export(t adapter.Target, a adapter.Adapter, items []library.Item, m manifest.Manifest) (exported, error) {
	caps := a.Capabilities()
	x := toolExport{
		tool:      a.ID(),
		originals: map[library.ID]library.Item{},
		held:      map[library.ID]bool{},
		secret:    map[library.ID]bool{},
	}
	for _, it := range items {
		x.originals[it.ID] = it
	}
	if caps.Kinds[library.KindPermission] {
		passed, gated := policy.Gate(items, x.tool, caps.Portable, e.Acks)
		for _, u := range gated {
			x.report(u)
		}
		items = passed
	}
	if caps.Kinds[library.KindMCP] {
		items = e.prepareServers(&x, items, caps.EnvRef)
	}
	units, unsupported, err := a.Export(t, items)
	if err != nil {
		return exported{}, fmt.Errorf("export %s for %s scope: %w", x.tool, t.Scope, err)
	}
	for _, u := range unsupported {
		x.report(e.acknowledge(u, x))
	}
	for _, u := range x.unsupported {
		if !slices.ContainsFunc(units, func(unit plan.Unit) bool { return slices.Contains(unit.Items, u.ItemID) }) {
			x.held[u.ItemID] = true
		}
	}
	units = slices.DeleteFunc(units, x.withholds)
	for i := range units {
		if slices.ContainsFunc(units[i].Items, func(id library.ID) bool { return x.secret[id] }) {
			units[i].Secret = true
		}
	}
	return exported{units: units, unsupported: x.unsupported, keep: x.kept(m)}, nil
}

func (x *toolExport) report(u adapter.Unsupported) {
	if u.Blocking {
		x.held[u.ItemID] = true
	}
	x.unsupported = append(x.unsupported, u)
}

func (x toolExport) withholds(u plan.Unit) bool {
	return slices.ContainsFunc(u.Items, func(id library.ID) bool { return x.held[id] })
}

func (x toolExport) kept(m manifest.Manifest) []manifest.Entry {
	var keep []manifest.Entry
	for _, entry := range m.ForTool(x.tool) {
		if slices.ContainsFunc(entry.Items, func(id library.ID) bool { return x.held[id] }) {
			keep = append(keep, entry)
		}
	}
	return keep
}

func (e Engine) acknowledge(u adapter.Unsupported, x toolExport) adapter.Unsupported {
	if !u.Blocking {
		return u
	}
	item, found := x.originals[u.ItemID]
	if found && e.Acks.Has(u.ItemID, cmp.Or(u.Tool, x.tool), library.ContentHash(item)) {
		u.Reason = acknowledgedPrefix + u.Reason
		u.Blocking = false
	}
	return u
}

func (e Engine) prepareServers(x *toolExport, items []library.Item, envRef func(string) string) []library.Item {
	prepared := make([]library.Item, 0, len(items))
	for _, it := range items {
		if it.Kind != library.KindMCP || it.MCP == nil {
			prepared = append(prepared, it)
			continue
		}
		server := *it.MCP
		if slices.Contains(e.Config.ResolveSecrets, it.Name) {
			server.Resolve = true
		}
		out, secretKeys, needsSetup, err := secrets.Prepare(server, envRef, e.resolver())
		unsupported := adapter.Unsupported{ItemID: it.ID, Name: it.Name, Tool: x.tool}
		switch {
		case err != nil:
			unsupported.Reason = reasonUnresolved + err.Error()
		case len(needsSetup) > 0:
			unsupported.Reason = ReasonNeedsSetup + strings.Join(needsSetup, ", ")
		default:
			it.MCP = &out
			if len(secretKeys) > 0 {
				x.secret[it.ID] = true
			}
			prepared = append(prepared, it)
			continue
		}
		x.held[it.ID] = true
		x.unsupported = append(x.unsupported, unsupported)
	}
	return prepared
}

type noResolver struct{}

func (noResolver) Resolve(string) (string, error) {
	return "", errors.New("no secret resolver configured")
}

func (e Engine) resolver() secrets.Resolver {
	if e.Resolver == nil {
		return noResolver{}
	}
	return e.Resolver
}
