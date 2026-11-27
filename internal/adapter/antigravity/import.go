package antigravity

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
)

const (
	noteSymlinked       = "symlinked -> %s; not imported"
	notePlugins         = "plugins may duplicate items PromptStor manages and may hold literal secrets; not read"
	noteWorkflows       = "deprecated workflows; not imported"
	noteSurfaceSkills   = "skills that only one Antigravity surface reads; not imported"
	noteDiscovery       = "discovery manifest; what it adds from other folders is not imported"
	noteLegacyFolder    = "legacy workspace folder; not imported"
	noteWorkspaceMemory = "workspace memory inside .agents; not imported"
	noteProjectHooks    = "project hooks are not managed; not imported"
	pluginsOwner        = "antigravity plugins"
	pluginsReason       = "installed or imported plugin; Antigravity manages it"
	importerOwner       = "antigravity importer"
	importerReason      = "record of imported setups; Antigravity writes it"
	builtinOwner        = "antigravity builtin"
	builtinReason       = "shipped with Antigravity and rewritten by it"
	builtinDir          = "builtin"
	globalWorkflows     = "global_workflows"
	workflowsDir        = "workflows"
)

var (
	discoveryManifests = []string{"skills.json", "rules.json", "agents.json", "plugins.json"}
	legacyWorkspaces   = []string{".agent", "_agents", "_agent"}
)

type importer struct {
	t   adapter.Target
	l   layout
	out adapter.Imported
}

func (Adapter) Import(t adapter.Target) (adapter.Imported, error) {
	l, ok := layoutFor(t)
	if !ok {
		return adapter.Imported{}, nil
	}
	im := &importer{t: t, l: l}
	steps := []func() error{
		im.rules,
		im.subagents,
		im.skills,
		im.permissions,
		im.hooks,
		im.servers,
		im.foreignState,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return adapter.Imported{}, err
		}
	}
	im.unmanaged()
	slices.SortStableFunc(im.out.Foreign, func(a, b adapter.Foreign) int { return strings.Compare(a.Path, b.Path) })
	slices.SortStableFunc(im.out.Notes, func(a, b adapter.Note) int { return strings.Compare(a.Path, b.Path) })
	return im.out, nil
}

func (im *importer) note(path, format string, args ...any) {
	im.out.Notes = append(im.out.Notes, adapter.Note{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (im *importer) foreign(path, owner, reason string) {
	im.out.Foreign = append(im.out.Foreign, adapter.Foreign{Path: path, Owner: owner, Reason: reason})
}

func (im *importer) symlinked(path string) {
	target, _ := os.Readlink(path)
	im.note(path, noteSymlinked, target)
}

func (im *importer) read(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if exists(path) {
			im.symlinked(path)
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	return data, true, nil
}

func (im *importer) entries(dir string) ([]fs.DirEntry, error) {
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", dir, err)
	case info.Mode()&fs.ModeSymlink != 0 && dir != im.l.config && dir != im.l.in(skillsDir):
		im.symlinked(dir)
		return nil, nil
	case info.Mode()&fs.ModeSymlink == 0 && !info.IsDir():
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		im.symlinked(dir)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	return entries, nil
}

func (im *importer) markdownFiles(dir string, visit func(path, name string) error) error {
	entries, err := im.entries(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		name, isMarkdown := strings.CutSuffix(entry.Name(), markdownExtension)
		switch {
		case strings.HasPrefix(entry.Name(), ".") || !isMarkdown:
		case entry.Type()&fs.ModeSymlink != 0:
			im.symlinked(path)
		case entry.Type().IsRegular():
			if err := visit(path, name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (im *importer) foreignState() error {
	if im.l.scope != adapter.ScopeUser {
		return nil
	}
	plugins := im.l.in(pluginsDir)
	entries, err := im.entries(plugins)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") {
			im.foreign(filepath.Join(plugins, entry.Name()), pluginsOwner, pluginsReason)
		}
	}
	if len(entries) > 0 {
		im.note(plugins, notePlugins)
	}
	if path := im.l.in(manifestName); exists(path) {
		im.foreign(path, importerOwner, importerReason)
	}
	if path := im.l.gemini(appDirName, builtinDir); exists(path) {
		im.foreign(path, builtinOwner, builtinReason)
	}
	return nil
}

func (im *importer) unmanaged() {
	notePresent := func(path, message string) {
		if exists(path) {
			im.note(path, "%s", message)
		}
	}
	for _, name := range discoveryManifests {
		notePresent(im.l.in(name), noteDiscovery)
	}
	notePresent(im.l.in(workflowsDir), noteWorkflows)
	if im.l.scope == adapter.ScopeUser {
		notePresent(im.l.in(globalWorkflows), noteWorkflows)
		notePresent(im.l.gemini(cliDirName, skillsDir), noteSurfaceSkills)
		notePresent(im.l.gemini(appDirName, skillsDir), noteSurfaceSkills)
		return
	}
	notePresent(im.l.in(hooksName), noteProjectHooks)
	notePresent(im.l.in(memoryName), noteWorkspaceMemory)
	notePresent(im.l.in(agentsName), noteWorkspaceMemory)
	for _, name := range legacyWorkspaces {
		notePresent(filepath.Join(im.l.root, name), noteLegacyFolder)
	}
}
