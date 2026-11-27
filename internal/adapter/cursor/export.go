package cursor

import (
	"cmp"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	fileMode             = fs.FileMode(0o644)
	reasonNoFileName     = "the name has no characters usable in a file name"
	reasonCredentialPath = "%s matches the credential exclusion list"
	reasonText           = "the item is not valid UTF-8"
)

type exporter struct {
	t           adapter.Target
	l           layout
	units       []plan.Unit
	unsupported []adapter.Unsupported
}

func (Adapter) Export(t adapter.Target, items []library.Item) ([]plan.Unit, []adapter.Unsupported, error) {
	l, ok := layoutFor(t)
	if !ok {
		return nil, nil, nil
	}
	ex := &exporter{t: t, l: l}
	servers, err := readExisting(t, l.in(mcpName))
	if err != nil {
		return nil, nil, err
	}
	hooks, err := readExisting(t, l.in(hooksName))
	if err != nil {
		return nil, nil, err
	}
	ex.rules(items)
	hookSet := newHookSet(hooks)
	for _, it := range items {
		switch it.Kind {
		case library.KindSkill:
			ex.skill(it)
		case library.KindCommand:
			ex.command(it)
		case library.KindSubagent:
			ex.subagent(it)
		case library.KindPermission:
			ex.permission(it)
		case library.KindHook:
			ex.hook(it, hookSet)
		case library.KindMCP:
			ex.server(it, rawObject(servers[serversKey]))
		}
	}
	ex.hooksVersion(hookSet)
	slices.SortStableFunc(ex.units, func(a, b plan.Unit) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Key, b.Key))
	})
	return ex.units, ex.unsupported, nil
}

func (ex *exporter) report(it library.Item, reason string) {
	ex.unsupported = append(ex.unsupported, adapter.Unsupported{ItemID: it.ID, Name: it.Name, Tool: tool, Reason: reason})
}

func (ex *exporter) block(it library.Item, reason string) {
	ex.unsupported = append(ex.unsupported, adapter.Unsupported{ItemID: it.ID, Name: it.Name, Tool: tool, Reason: reason, Blocking: true})
}

func (ex *exporter) add(u plan.Unit) {
	u.Tool = tool
	u.Root = ex.l.root
	u.Mode = fileMode
	ex.units = append(ex.units, u)
}

func (ex *exporter) document(it library.Item, path string, content []byte) bool {
	if backup.Excluded(ex.t.Env, path) {
		ex.report(it, fmt.Sprintf(reasonCredentialPath, filepath.Base(path)))
		return false
	}
	ex.add(plan.Unit{Path: path, Kind: manifest.UnitFile, Content: content, Items: []library.ID{it.ID}})
	return true
}
