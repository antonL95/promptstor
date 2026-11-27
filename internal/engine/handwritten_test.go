package engine_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/testenv"
)

var (
	storedID    = regexp.MustCompile(`(?m)^  id: (ps_[0-9A-Z]{26})$`)
	temporaryID = regexp.MustCompile(`^ps_tmp_[a-z2-7]{26}$`)
)

func handWritten(t *testing.T, env paths.Env) (project string, files map[library.Layer]string) {
	t.Helper()
	project = t.TempDir()
	files = map[library.Layer]string{
		library.LayerPersonal: filepath.Join(env.LibraryDir(), "rules", "hand-personal.md"),
		library.LayerDevice:   filepath.Join(env.DeviceDir(), "rules", "hand-device.md"),
		library.LayerProject:  filepath.Join(project, ".promptstor", "rules", "hand-project.md"),
	}
	for layer, path := range files {
		writeFile(t, path, "Hand written "+string(layer)+" rule.\n")
	}
	return project, files
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func handWrittenItem(t *testing.T, e engine.Engine, project, name string) library.Item {
	t.Helper()
	merged, _, err := e.Library(project)
	if err != nil {
		t.Fatal(err)
	}
	at := slices.IndexFunc(merged.Items, func(it library.Item) bool { return it.Name == name })
	if at < 0 {
		t.Fatalf("no item named %s", name)
	}
	return merged.Items[at]
}

func TestSyncWritesIDsIntoHandWrittenItems(t *testing.T) {
	env := home(t)
	project, files := handWritten(t, env)
	e := newEngine(env, adapter.ClaudeCode)

	if _, _, err := e.Sync(project, plan.Options{}); err != nil {
		t.Fatal(err)
	}

	m, err := manifest.Load(env.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	stored := map[library.Layer]string{}
	for layer, path := range files {
		content := readText(t, path)
		found := storedID.FindStringSubmatch(content)
		if found == nil || !strings.HasSuffix(content, "Hand written "+string(layer)+" rule.\n") {
			t.Fatalf("%s item:\n%s", layer, content)
		}
		stored[layer] = content
		if !slices.ContainsFunc(m.Entries, func(entry manifest.Entry) bool {
			return slices.Contains(entry.Items, library.ID(found[1]))
		}) {
			t.Errorf("%s id %s is in no manifest entry", layer, found[1])
		}
	}

	again, _, err := e.Sync(project, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Written) != 0 {
		t.Errorf("second sync wrote %v", again.Written)
	}
	for layer, path := range files {
		if readText(t, path) != stored[layer] {
			t.Errorf("second sync changed the %s item", layer)
		}
	}
}

func TestDryRunNeverWritesIDs(t *testing.T) {
	env := home(t)
	project, _ := handWritten(t, env)
	e := newEngine(env, adapter.ClaudeCode)
	homeBefore, projectBefore := testenv.TreeHash(t, env.Home), testenv.TreeHash(t, project)

	if _, err := e.Plan(project, plan.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := drift.Check(e, project); err != nil {
		t.Fatal(err)
	}
	first := handWrittenItem(t, e, project, "hand-personal")
	second := handWrittenItem(t, e, project, "hand-personal")
	device := handWrittenItem(t, e, project, "hand-device")

	if !temporaryID.MatchString(string(first.ID)) || first.ID != second.ID || first.ID == device.ID {
		t.Errorf("temporary ids %q, %q and %q", first.ID, second.ID, device.ID)
	}
	if testenv.TreeHash(t, env.Home) != homeBefore || testenv.TreeHash(t, project) != projectBefore {
		t.Error("a read only call changed files")
	}
}

func TestSyncNeverWritesIDsThroughSymlinkedLibrary(t *testing.T) {
	env := home(t)
	elsewhere := t.TempDir()
	writeFile(t, filepath.Join(elsewhere, "rules", "hand-device.md"), "Linked rule.\n")
	if err := os.Symlink(elsewhere, env.DeviceDir()); err != nil {
		t.Fatal(err)
	}
	before := testenv.TreeHash(t, elsewhere)

	_, p, err := newEngine(env, adapter.ClaudeCode).Sync("", plan.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if testenv.TreeHash(t, elsewhere) != before {
		t.Error("wrote through the symlinked device layer")
	}
	if !slices.ContainsFunc(p.Unsupported, func(u adapter.Unsupported) bool { return u.Name == env.DeviceDir() }) {
		t.Errorf("the symlink is not reported: %+v", p.Unsupported)
	}
}
