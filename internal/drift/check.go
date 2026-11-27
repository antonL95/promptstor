package drift

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	detailHandEdit  = "changed outside promptstor"
	detailNotOnDisk = "not on disk"
	detailUnwritten = "not written yet"
	detailReplace   = "differs from the library"
	detailChanged   = "library changed"
	detailRemove    = "to be removed"
)

type handEditDetailer interface {
	HandEditDetail(t adapter.Target, path string, kind manifest.UnitKind) string
}

type checker struct {
	e        engine.Engine
	targets  []adapter.Target
	manifest manifest.Manifest
	names    map[library.ID]string
}

func Check(e engine.Engine, projectRoot string) (Report, error) {
	p, err := e.Plan(projectRoot, plan.Options{})
	if err != nil {
		return Report{}, err
	}
	m, err := manifest.Load(e.Env.ManifestPath())
	if err != nil {
		return Report{}, err
	}
	merged, _, err := e.Library(projectRoot)
	if err != nil {
		return Report{}, err
	}
	targets, err := e.Targets(projectRoot)
	if err != nil {
		return Report{}, err
	}
	c := checker{e: e, targets: targets, manifest: m, names: map[library.ID]string{}}
	for _, it := range merged.Items {
		c.names[it.ID] = it.Name
	}
	var findings []Finding
	for _, change := range p.Changes {
		found, err := c.change(change)
		if err != nil {
			return Report{}, err
		}
		findings = append(findings, found...)
	}
	held, err := c.held(p.Changes)
	if err != nil {
		return Report{}, err
	}
	findings = append(findings, held...)
	findings = append(findings, unsupportedFindings(p.Unsupported)...)
	findings = append(findings, violationFindings(p.Violations)...)
	imported, err := c.imports()
	if err != nil {
		return Report{}, err
	}
	findings = append(findings, imported...)
	findings = append(findings, c.skillHomes(p.Changes)...)
	return Report{Generated: now(e), LibraryHash: p.LibraryHash, Findings: sorted(findings)}, nil
}

func now(e engine.Engine) time.Time {
	if e.Now == nil {
		return time.Now()
	}
	return e.Now()
}

func (c checker) change(fc plan.FileChange) ([]Finding, error) {
	if fc.Blocked != "" {
		return []Finding{blockedFinding(fc)}, nil
	}
	var findings []Finding
	for _, u := range fc.Units {
		f, found, err := c.unit(fc, u)
		if err != nil {
			return nil, err
		}
		if found {
			findings = append(findings, f)
		}
	}
	for _, entry := range fc.Removed {
		f, found, err := c.removed(fc, entry)
		if err != nil {
			return nil, err
		}
		if found {
			findings = append(findings, f)
		}
	}
	return findings, nil
}

func (c checker) unit(fc plan.FileChange, u plan.Unit) (Finding, bool, error) {
	want, err := manifest.Hash(u.Kind, u.Content)
	if err != nil {
		return Finding{}, false, fmt.Errorf("check %s: %w", u.Path, err)
	}
	have, onDisk, err := diskHash(fc, u.Kind, u.Key)
	if err != nil {
		return Finding{}, false, err
	}
	entry, owned := c.manifest.Get(u.Path, u.Kind, u.Key)
	described := func(kind FindingKind, detail string) (Finding, bool, error) {
		return c.unitFinding(kind, u.Tool, u.Path, u.Key, u.Items, u.Secret, detail), true, nil
	}
	switch {
	case onDisk && have == want:
		return Finding{}, false, nil
	case !owned && !onDisk:
		return described(KindPending, detailUnwritten)
	case !owned:
		return described(KindPending, detailReplace)
	case !onDisk:
		return described(KindMissing, detailNotOnDisk)
	case have != entry.Hash:
		return described(KindHandEdit, c.handEditDetail(u.Tool, u.Root, u.Path, u.Kind))
	default:
		return described(KindPending, detailChanged)
	}
}

func (c checker) removed(fc plan.FileChange, entry manifest.Entry) (Finding, bool, error) {
	if entry.Adopted {
		return Finding{}, false, nil
	}
	have, onDisk, err := diskHash(fc, entry.Unit, entry.Key)
	if err != nil || !onDisk {
		return Finding{}, false, err
	}
	detail := detailRemove
	kind := KindPending
	if have != entry.Hash {
		kind = KindHandEdit
		detail = c.handEditDetail(entry.Tool, entry.Root, entry.Path, entry.Unit)
	}
	return c.unitFinding(kind, entry.Tool, entry.Path, entry.Key, entry.Items, entry.Secret, detail), true, nil
}

func diskHash(fc plan.FileChange, kind manifest.UnitKind, key string) (string, bool, error) {
	current, found, err := plan.ExtractUnit(kind, fc.Before, key, fc.Exists)
	if err != nil {
		return "", false, fmt.Errorf("check %s: %w", fc.Path, err)
	}
	if !found {
		return "", false, nil
	}
	hash, err := manifest.Hash(kind, current)
	if err != nil {
		return "", false, fmt.Errorf("check %s: %w", fc.Path, err)
	}
	return hash, true, nil
}

func (c checker) unitFinding(kind FindingKind, tool, path, key string, items []library.ID, secret bool, detail string) Finding {
	f := Finding{Kind: kind, Tool: tool, Path: path, Detail: detail}
	if key != "" {
		f.Detail = key + ": " + detail
	}
	if len(items) > 0 {
		f.Item = items[0]
	}
	var names []string
	for _, id := range items {
		if name := c.names[id]; name != "" {
			names = append(names, name)
		}
	}
	if len(names) > 0 {
		slices.Sort(names)
		f.Detail += " (" + strings.Join(slices.Compact(names), ", ") + ")"
	}
	if secret {
		f.Detail += " " + secretMarker
	}
	return f
}

func (c checker) handEditDetail(tools, root, path string, kind manifest.UnitKind) string {
	for _, tool := range strings.Split(tools, ",") {
		a, found := c.adapter(tool)
		if !found {
			continue
		}
		detailer, ok := a.(handEditDetailer)
		if !ok {
			continue
		}
		for _, t := range c.targets {
			if targetRoot(t) != root {
				continue
			}
			if detail := detailer.HandEditDetail(t, path, kind); detail != "" {
				return detail
			}
		}
	}
	return detailHandEdit
}

func (c checker) adapter(tool string) (adapter.Adapter, bool) {
	at := slices.IndexFunc(c.e.Adapters, func(a adapter.Adapter) bool { return a.ID() == tool })
	if at < 0 {
		return nil, false
	}
	return c.e.Adapters[at], true
}

func targetRoot(t adapter.Target) string {
	if t.Scope == adapter.ScopeProject {
		return t.ProjectRoot
	}
	return t.Env.Home
}

func blockedFinding(fc plan.FileChange) Finding {
	kind := KindUnsupported
	switch {
	case strings.HasPrefix(fc.Blocked, plan.ReasonSymlinked):
		kind = KindSymlinked
	case strings.HasPrefix(fc.Blocked, plan.ReasonConflict):
		kind = KindSharedConflict
	}
	tools := map[string]bool{}
	f := Finding{Kind: kind, Path: fc.Path, Detail: fc.Blocked}
	for _, u := range fc.Units {
		for _, tool := range strings.Split(u.Tool, ",") {
			tools[tool] = true
		}
		if f.Item == "" && len(u.Items) > 0 {
			f.Item = u.Items[0]
		}
	}
	for _, entry := range fc.Removed {
		for _, tool := range entry.Tools() {
			tools[tool] = true
		}
	}
	f.Tool = strings.Join(slices.Sorted(maps.Keys(tools)), ",")
	return f
}

func unsupportedFindings(report []adapter.Unsupported) []Finding {
	findings := make([]Finding, 0, len(report))
	for _, u := range report {
		kind := KindUnsupported
		switch {
		case u.Blocking:
			kind = KindAckRequired
		case strings.HasPrefix(u.Reason, engine.ReasonNeedsSetup):
			kind = KindNeedsSetup
		}
		findings = append(findings, Finding{Kind: kind, Tool: u.Tool, Item: u.ItemID, Name: u.Name, Detail: u.Reason})
	}
	return findings
}

func violationFindings(violations []library.Violation) []Finding {
	findings := make([]Finding, 0, len(violations))
	for _, v := range violations {
		findings = append(findings, Finding{
			Kind:   KindLockViolation,
			Item:   v.ItemID,
			Name:   v.Name,
			Detail: fmt.Sprintf("%s layer overrides an item %s", v.Layer, v.Reason),
		})
	}
	return findings
}
