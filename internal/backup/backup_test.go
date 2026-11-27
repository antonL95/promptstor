package backup_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/structedit"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	credentialMarker = "PSFIXTURE-CREDENTIAL-7Q2X"
	redactMarker     = "PSFIXTURE-REDACT-4K9M"
	claudeTool       = "claude-code"
	codexTool        = "codex"
)

var _ plan.Snapshotter = backup.Store{}

var idPattern = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{4}$`)

func home(t *testing.T) paths.Env {
	t.Helper()
	env := testenv.Home(t, "testdata/home")
	env.StateHome = filepath.Join(t.TempDir(), "state")
	return env
}

func store(env paths.Env) backup.Store {
	clock := time.Date(2026, 11, 27, 14, 22, 33, 0, time.UTC)
	return backup.Store{Env: env, Now: func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}}
}

func at(env paths.Env, rel string) string {
	return filepath.Join(env.Home, filepath.FromSlash(rel))
}

func entry(env paths.Env, tool, rel string) plan.SnapshotEntry {
	return plan.SnapshotEntry{Tool: tool, Path: at(env, rel)}
}

func take(t *testing.T, s backup.Store, reason backup.Reason, entries ...plan.SnapshotEntry) backup.Snapshot {
	t.Helper()
	snap, err := s.Take(reason, entries)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func restore(t *testing.T, s backup.Store, id, tool string, dryRun bool) backup.RestorePlan {
	t.Helper()
	restored, err := s.Restore(id, tool, dryRun)
	if err != nil {
		t.Fatal(err)
	}
	return restored
}

func fileAt(t *testing.T, snap backup.Snapshot, path string) backup.SnapFile {
	t.Helper()
	for _, f := range snap.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("snapshot has no file %s", path)
	return backup.SnapFile{}
}

func blob(t *testing.T, env paths.Env, f backup.SnapFile) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(env.BackupDir(), "blobs", f.Blob))
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

func jsonValue(t *testing.T, path, key string) (string, bool) {
	t.Helper()
	value, found, err := structedit.JSONGet([]byte(read(t, path)), key)
	if err != nil {
		t.Fatal(err)
	}
	return string(value), found
}

func assertNoMarkerStored(t *testing.T, env paths.Env, markers ...string) {
	t.Helper()
	err := filepath.WalkDir(env.BackupDir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, marker := range markers {
			if bytes.Contains(content, []byte(marker)) {
				t.Errorf("%s holds a fixture secret marker", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFirstRunSnapshotContainsAllEntries(t *testing.T) {
	env := home(t)
	s := store(env)
	snap := take(t, s, backup.ReasonFirstRun,
		entry(env, claudeTool, ".claude/settings.json"),
		entry(env, claudeTool, ".claude/skills"),
		entry(env, claudeTool, ".claude/agents/reviewer.md"),
		entry(env, codexTool, ".codex/config.toml"),
	)

	if !idPattern.MatchString(snap.ID) || snap.Reason != backup.ReasonFirstRun {
		t.Fatalf("id %q reason %q", snap.ID, snap.Reason)
	}
	want := map[string]backup.FileKind{
		".claude/settings.json":                 backup.KindFile,
		".claude/skills":                        backup.KindDir,
		".claude/skills/demo-skill":             backup.KindDir,
		".claude/skills/demo-skill/SKILL.md":    backup.KindFile,
		".claude/skills/demo-skill/notes.md":    backup.KindFile,
		".claude/skills/linked-skill":           backup.KindSymlink,
		".claude/skills/token-counter":          backup.KindDir,
		".claude/skills/token-counter/SKILL.md": backup.KindFile,
		".claude/agents/reviewer.md":            backup.KindAbsent,
		".codex/config.toml":                    backup.KindFile,
	}
	if len(snap.Files) != len(want) {
		t.Errorf("snapshot has %d files, want %d: %+v", len(snap.Files), len(want), snap.Files)
	}
	for rel, kind := range want {
		f := fileAt(t, snap, at(env, rel))
		if f.Kind != kind {
			t.Errorf("%s kind %q, want %q", rel, f.Kind, kind)
		}
		wantTool := claudeTool
		if strings.HasPrefix(rel, ".codex") {
			wantTool = codexTool
		}
		if f.Tool != wantTool {
			t.Errorf("%s tool %q", rel, f.Tool)
		}
	}
	settings := fileAt(t, snap, at(env, ".claude/settings.json"))
	if got := string(blob(t, env, settings)); got != read(t, at(env, ".claude/settings.json")) {
		t.Errorf("settings blob differs: %s", got)
	}
	if settings.Mode.Perm() != 0o644 {
		t.Errorf("settings mode %v", settings.Mode)
	}
	if link := fileAt(t, snap, at(env, ".claude/skills/linked-skill")); link.LinkTarget != "../../shared-skills/linked-skill" {
		t.Errorf("link target %q", link.LinkTarget)
	}

	listed, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != snap.ID || len(listed[0].Files) != len(snap.Files) {
		t.Errorf("list %+v", listed)
	}
}

func TestSnapshotSatisfiesSnapshotter(t *testing.T) {
	env := home(t)
	s := store(env)
	id, err := s.Snapshot("change", []plan.SnapshotEntry{entry(env, claudeTool, ".claude/settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != id || listed[0].Reason != backup.ReasonChange {
		t.Errorf("list %+v, id %q", listed, id)
	}
}

func TestCredentialFilesNeverStored(t *testing.T) {
	env := home(t)
	s := store(env)
	snap := take(t, s, backup.ReasonFirstRun,
		entry(env, claudeTool, ".claude"),
		entry(env, claudeTool, ".claude.json"),
		plan.SnapshotEntry{Tool: claudeTool, Path: at(env, ".claude.json"), Keys: []string{"/oauthAccount", "/primaryApiKey/x", "/mcpServers"}},
		entry(env, codexTool, ".codex"),
		entry(env, "gemini", ".gemini/oauth_creds.json"),
		entry(env, "opencode", ".config/opencode"),
	)

	assertNoMarkerStored(t, env, credentialMarker)
	if want := []string{at(env, ".gemini/oauth_creds.json")}; !slices.Equal(snap.Skipped, want) {
		t.Errorf("skipped %v, want %v", snap.Skipped, want)
	}
	for _, f := range snap.Files {
		if backup.Excluded(env, f.Path) {
			t.Errorf("excluded path %s recorded", f.Path)
		}
	}
	for _, rel := range []string{".codex/auth.json", ".claude/.credentials.json", ".claude/backups", ".config/opencode/service.json"} {
		for _, f := range snap.Files {
			if f.Path == at(env, rel) || strings.HasPrefix(f.Path, at(env, rel)+string(filepath.Separator)) {
				t.Errorf("%s recorded", f.Path)
			}
		}
	}
	if fileAt(t, snap, at(env, ".codex/config.toml")).Kind != backup.KindFile {
		t.Error("codex config missing")
	}
	var whole, keyed backup.SnapFile
	for _, f := range snap.Files {
		if f.Path == at(env, ".claude.json") && f.Keys == nil {
			whole = f
		}
		if f.Path == at(env, ".claude.json") && f.Keys != nil {
			keyed = f
		}
	}
	stored := blob(t, env, whole)
	if _, found, _ := structedit.JSONGet(stored, "/mcpServers/demo/command"); !found {
		t.Errorf("whole .claude.json lost mcpServers: %s", stored)
	}
	if !slices.Equal(sortedKeys(keyed.Keys), []string{"/mcpServers"}) {
		t.Errorf("keyed .claude.json stored keys %v", sortedKeys(keyed.Keys))
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestExcludedPointersKeptOnRestore(t *testing.T) {
	env := home(t)
	s := store(env)
	snap := take(t, s, backup.ReasonChange, entry(env, claudeTool, ".claude.json"))
	path := at(env, ".claude.json")
	write(t, path, `{"numStartups": 9, "oauthAccount": {"accountUuid": "current-account"}}`)

	restore(t, s, snap.ID, "", false)

	if got, _ := jsonValue(t, path, "/oauthAccount/accountUuid"); got != `"current-account"` {
		t.Errorf("oauthAccount %s", got)
	}
	if got, _ := jsonValue(t, path, "/numStartups"); got != "3" {
		t.Errorf("numStartups %s", got)
	}
	if _, found := jsonValue(t, path, "/primaryApiKey"); found {
		t.Error("primaryApiKey came back from nowhere")
	}
}

func TestOpencodeShareDirNeverStored(t *testing.T) {
	env := home(t)
	s := store(env)
	snap := take(t, s, backup.ReasonFirstRun,
		entry(env, "opencode", ".local/share/opencode"),
		entry(env, "opencode", ".local/share/opencode/storage/session.json"),
		entry(env, "opencode", ".local/share"),
	)

	assertNoMarkerStored(t, env, credentialMarker)
	want := []string{at(env, ".local/share/opencode"), at(env, ".local/share/opencode/storage/session.json")}
	if !slices.Equal(snap.Skipped, want) {
		t.Errorf("skipped %v", snap.Skipped)
	}
	for _, f := range snap.Files {
		if strings.Contains(f.Path, "opencode") {
			t.Errorf("%s recorded", f.Path)
		}
	}
}

func TestSymlinkRestoredAsLink(t *testing.T) {
	env := home(t)
	s := store(env)
	snap := take(t, s, backup.ReasonChange, entry(env, claudeTool, ".claude/skills/linked-skill"), entry(env, claudeTool, ".claude/CLAUDE.md"))
	link := at(env, ".claude/skills/linked-skill")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../shared-skills/other-skill", link); err != nil {
		t.Fatal(err)
	}
	rules := at(env, ".claude/CLAUDE.md")
	if err := os.Remove(rules); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../shared-skills/other-skill/SKILL.md", rules); err != nil {
		t.Fatal(err)
	}

	restore(t, s, snap.ID, claudeTool, false)

	target, err := os.Readlink(link)
	if err != nil || target != "../../shared-skills/linked-skill" {
		t.Errorf("link target %q, %v", target, err)
	}
	info, err := os.Lstat(rules)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("rules not a regular file: %v %v", info, err)
	}
	if got := read(t, rules); !strings.Contains(got, "Keep answers short.") {
		t.Errorf("rules %q", got)
	}
	if got := read(t, at(env, "shared-skills/other-skill/SKILL.md")); strings.Contains(got, "Keep answers short.") {
		t.Error("restore wrote through the link")
	}
}

func TestAbsentFileDeletedOnRestore(t *testing.T) {
	env := home(t)
	s := store(env)
	snap := take(t, s, backup.ReasonChange, entry(env, claudeTool, ".claude/agents/reviewer.md"), entry(env, claudeTool, ".claude/agents/linked.md"))
	write(t, at(env, ".claude/agents/reviewer.md"), "added later\n")
	if err := os.Symlink("../CLAUDE.md", at(env, ".claude/agents/linked.md")); err != nil {
		t.Fatal(err)
	}

	restored := restore(t, s, snap.ID, "", false)

	for _, rel := range []string{".claude/agents/reviewer.md", ".claude/agents/linked.md"} {
		if exists(t, at(env, rel)) {
			t.Errorf("%s still exists", rel)
		}
	}
	if !exists(t, at(env, ".claude/CLAUDE.md")) {
		t.Error("link target deleted")
	}
	if ops := opsOf(restored); !slices.Equal(ops, []string{"delete", "delete"}) {
		t.Errorf("ops %v", ops)
	}
}

func opsOf(p backup.RestorePlan) []string {
	ops := make([]string, 0, len(p.Actions))
	for _, a := range p.Actions {
		ops = append(ops, a.Op)
	}
	return ops
}

func TestFilesAddedToSkillDirRemovedOnRestore(t *testing.T) {
	env := home(t)
	s := store(env)
	skills := at(env, ".claude/skills")
	before := testenv.TreeHash(t, skills)
	snap := take(t, s, backup.ReasonChange, entry(env, claudeTool, ".claude/skills"))
	write(t, at(env, ".claude/skills/demo-skill/extra.md"), "added later\n")
	write(t, at(env, ".claude/skills/new-skill/SKILL.md"), "added later\n")
	write(t, at(env, ".claude/skills/demo-skill/SKILL.md"), "changed\n")
	if err := os.Chmod(at(env, ".claude/skills/demo-skill/SKILL.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(at(env, ".claude/skills/demo-skill/notes.md")); err != nil {
		t.Fatal(err)
	}

	restored := restore(t, s, snap.ID, claudeTool, false)

	if after := testenv.TreeHash(t, skills); after != before {
		t.Errorf("skills differ after restore, actions %+v", restored.Actions)
	}
	if !slices.Contains(opsOf(restored), "prune") {
		t.Errorf("no prune action: %v", opsOf(restored))
	}
}

func TestExcludedFileInSkillDirSurvivesRestore(t *testing.T) {
	env := home(t)
	s := store(env)
	snap := take(t, s, backup.ReasonChange, entry(env, claudeTool, ".claude/skills"))
	added := at(env, ".claude/skills/new-skill/api-token.txt")
	write(t, added, "kept\n")

	restore(t, s, snap.ID, "", false)

	if read(t, added) != "kept\n" {
		t.Error("excluded file touched")
	}
}

func TestRestoreTakesPreRestoreSnapshot(t *testing.T) {
	env := home(t)
	s := store(env)
	path := at(env, ".claude/CLAUDE.md")
	original := read(t, path)
	snap := take(t, s, backup.ReasonChange, entry(env, claudeTool, ".claude/CLAUDE.md"))
	write(t, path, "edited by hand\n")

	restored := restore(t, s, snap.ID, "", false)

	if read(t, path) != original {
		t.Error("not restored")
	}
	listed, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].ID != restored.PreRestoreID || listed[0].Reason != backup.ReasonPreRestore {
		t.Fatalf("list %+v, pre-restore %q", listed, restored.PreRestoreID)
	}
	if got := string(blob(t, env, fileAt(t, listed[0], path))); got != "edited by hand\n" {
		t.Errorf("pre-restore blob %q", got)
	}
	if restored.Snapshot.ID != snap.ID {
		t.Errorf("plan snapshot %q", restored.Snapshot.ID)
	}
}

func TestRestoreOneToolOnly(t *testing.T) {
	env := home(t)
	s := store(env)
	snap := take(t, s, backup.ReasonChange,
		entry(env, claudeTool, ".claude/CLAUDE.md"),
		entry(env, codexTool, ".codex/config.toml"),
		entry(env, claudeTool+","+codexTool, ".codex/AGENTS.md"),
	)
	for _, rel := range []string{".claude/CLAUDE.md", ".codex/config.toml", ".codex/AGENTS.md"} {
		write(t, at(env, rel), "edited\n")
	}

	restored := restore(t, s, snap.ID, codexTool, false)

	if read(t, at(env, ".claude/CLAUDE.md")) != "edited\n" {
		t.Error("claude file restored")
	}
	for _, rel := range []string{".codex/config.toml", ".codex/AGENTS.md"} {
		if read(t, at(env, rel)) == "edited\n" {
			t.Errorf("%s not restored", rel)
		}
	}
	listed, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	pre := listed[0]
	if pre.ID != restored.PreRestoreID || len(pre.Files) != 2 {
		t.Errorf("pre-restore snapshot %+v", pre)
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	env := home(t)
	s := store(env)
	snap := take(t, s, backup.ReasonChange,
		entry(env, claudeTool, ".claude/skills"),
		entry(env, claudeTool, ".claude/CLAUDE.md"),
		entry(env, claudeTool, ".claude/agents/reviewer.md"),
	)
	write(t, at(env, ".claude/CLAUDE.md"), "edited\n")
	write(t, at(env, ".claude/agents/reviewer.md"), "added\n")
	write(t, at(env, ".claude/skills/demo-skill/extra.md"), "added\n")
	homeBefore := testenv.TreeHash(t, env.Home)
	stateBefore := testenv.TreeHash(t, env.StateHome)

	restored := restore(t, s, snap.ID, "", true)

	if testenv.TreeHash(t, env.Home) != homeBefore || testenv.TreeHash(t, env.StateHome) != stateBefore {
		t.Error("dry run changed files")
	}
	if restored.PreRestoreID != "" {
		t.Errorf("dry run took snapshot %q", restored.PreRestoreID)
	}
	want := []backup.RestoreAction{
		{Path: at(env, ".claude/skills/demo-skill/extra.md"), Op: "prune"},
		{Path: at(env, ".claude/CLAUDE.md"), Op: "write"},
		{Path: at(env, ".claude/agents/reviewer.md"), Op: "delete"},
	}
	var got []backup.RestoreAction
	for _, a := range restored.Actions {
		got = append(got, backup.RestoreAction{Path: a.Path, Op: a.Op})
	}
	if !slices.Equal(got, want) {
		t.Errorf("actions %+v\nwant %+v", got, want)
	}
}

func TestKeyLevelEntryTouchesOnlyItsKeys(t *testing.T) {
	env := home(t)
	s := store(env)
	path := at(env, ".claude.json")
	snap := take(t, s, backup.ReasonChange, plan.SnapshotEntry{Tool: claudeTool, Path: path, Keys: []string{"/mcpServers/demo/command", "/projects"}})
	f := fileAt(t, snap, path)
	if f.Keys["/projects"] != "" || f.Keys["/mcpServers/demo/command"] != `"demo-server"` || f.Blob != "" {
		t.Fatalf("stored %+v", f)
	}
	edited := `{
  "numStartups": 4,
  "oauthAccount": {"accountUuid": "current-account"},
  "projects": {"/work": {}},
  "mcpServers": {"demo": {"command": "edited-server", "args": ["x"]}}
}
`
	write(t, path, edited)

	restored := restore(t, s, snap.ID, "", false)

	if got, _ := jsonValue(t, path, "/mcpServers/demo/command"); got != `"demo-server"` {
		t.Errorf("command %s", got)
	}
	if _, found := jsonValue(t, path, "/projects"); found {
		t.Error("projects not deleted")
	}
	if got, _ := jsonValue(t, path, "/numStartups"); got != "4" {
		t.Errorf("numStartups %s", got)
	}
	if got, _ := jsonValue(t, path, "/mcpServers/demo/args/0"); got != `"x"` {
		t.Errorf("args %s", got)
	}
	if got, _ := jsonValue(t, path, "/oauthAccount/accountUuid"); got != `"current-account"` {
		t.Errorf("oauthAccount %s", got)
	}
	if !slices.Equal(opsOf(restored), []string{"set-keys"}) {
		t.Errorf("ops %v", opsOf(restored))
	}
}

func TestRedactedSecretNotInAnyBlob(t *testing.T) {
	env := home(t)
	s := store(env)
	snap := take(t, s, backup.ReasonChange,
		plan.SnapshotEntry{Tool: claudeTool, Path: at(env, ".claude.json"), Keys: []string{"/mcpServers"}, RedactKeys: []string{"/mcpServers/demo/env/API_KEY"}},
		plan.SnapshotEntry{Tool: claudeTool, Path: at(env, ".claude/settings.json"), RedactKeys: []string{"/env/SERVICE_KEY", "/env/MISSING"}},
	)

	assertNoMarkerStored(t, env, redactMarker, credentialMarker)
	keyed := fileAt(t, snap, at(env, ".claude.json"))
	if !slices.Equal(keyed.Redacted, []string{"/mcpServers/demo/env/API_KEY"}) {
		t.Errorf("redacted %v", keyed.Redacted)
	}
	if !strings.Contains(keyed.Keys["/mcpServers"], `"<promptstor:redacted>"`) {
		t.Errorf("stored %s", keyed.Keys["/mcpServers"])
	}
	whole := fileAt(t, snap, at(env, ".claude/settings.json"))
	if !slices.Equal(whole.Redacted, []string{"/env/SERVICE_KEY"}) {
		t.Errorf("redacted %v", whole.Redacted)
	}
	if got, _, _ := structedit.JSONGet(blob(t, env, whole), "/env/SERVICE_KEY"); string(got) != `"<promptstor:redacted>"` {
		t.Errorf("blob value %s", got)
	}
}

func TestRedactedSecretResolvedOnRestore(t *testing.T) {
	env := home(t)
	s := store(env)
	path := at(env, ".claude/settings.json")
	snap := take(t, s, backup.ReasonChange, plan.SnapshotEntry{Tool: claudeTool, Path: path, RedactKeys: []string{"/env/SERVICE_KEY"}})
	write(t, path, `{"model": "edited"}`)
	var asked []string
	s.Resolve = func(tool, file, key string) ([]byte, error) {
		asked = append(asked, tool+" "+file+" "+key)
		return []byte(`"resolved-value"`), nil
	}

	restored := restore(t, s, snap.ID, "", false)

	if got, _ := jsonValue(t, path, "/env/SERVICE_KEY"); got != `"resolved-value"` {
		t.Errorf("secret %s", got)
	}
	if got, _ := jsonValue(t, path, "/model"); got != `"fixture-model"` {
		t.Errorf("model %s", got)
	}
	if want := []string{claudeTool + " " + path + " /env/SERVICE_KEY"}; !slices.Equal(asked, want) {
		t.Errorf("resolve calls %v", asked)
	}
	if len(restored.Unresolved) != 0 {
		t.Errorf("unresolved %v", restored.Unresolved)
	}
}

func TestUnresolvedSecretLeftAsOnDisk(t *testing.T) {
	env := home(t)
	s := store(env)
	settings := at(env, ".claude/settings.json")
	claudeJSON := at(env, ".claude.json")
	snap := take(t, s, backup.ReasonChange,
		plan.SnapshotEntry{Tool: claudeTool, Path: settings, RedactKeys: []string{"/env/SERVICE_KEY"}},
		plan.SnapshotEntry{Tool: claudeTool, Path: claudeJSON, Keys: []string{"/mcpServers"}, RedactKeys: []string{"/mcpServers/demo/env/API_KEY"}},
	)
	write(t, settings, `{"model": "edited", "env": {"SERVICE_KEY": "current-value"}}`)
	write(t, claudeJSON, `{"mcpServers": {}}`)
	s.Resolve = func(string, string, string) ([]byte, error) { return nil, errors.New("no secret store") }

	restored := restore(t, s, snap.ID, "", false)

	if got, _ := jsonValue(t, settings, "/env/SERVICE_KEY"); got != `"current-value"` {
		t.Errorf("settings secret %s", got)
	}
	if got, _ := jsonValue(t, settings, "/model"); got != `"fixture-model"` {
		t.Errorf("model %s", got)
	}
	if got, _ := jsonValue(t, claudeJSON, "/mcpServers/demo/command"); got != `"demo-server"` {
		t.Errorf("command %s", got)
	}
	if _, found := jsonValue(t, claudeJSON, "/mcpServers/demo/env/API_KEY"); found {
		t.Error("placeholder or stale secret written")
	}
	want := []string{claudeJSON + " /mcpServers/demo/env/API_KEY", settings + " /env/SERVICE_KEY"}
	if !slices.Equal(restored.Unresolved, want) {
		t.Errorf("unresolved %v, want %v", restored.Unresolved, want)
	}
	for _, path := range []string{settings, claudeJSON} {
		if strings.Contains(read(t, path), "<promptstor:redacted>") {
			t.Errorf("%s holds the placeholder", path)
		}
	}
}

func TestRestoreBlocksSymlinkedParent(t *testing.T) {
	env := home(t)
	s := store(env)
	snap := take(t, s, backup.ReasonChange, entry(env, claudeTool, ".claude/agents/reviewer.md"), entry(env, claudeTool, ".claude/CLAUDE.md"))
	if err := os.Mkdir(at(env, "elsewhere"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, at(env, "elsewhere/reviewer.md"), "outside\n")
	if err := os.Symlink("../elsewhere", at(env, ".claude/agents")); err != nil {
		t.Fatal(err)
	}
	write(t, at(env, ".claude/CLAUDE.md"), "edited\n")

	restored := restore(t, s, snap.ID, "", false)

	if read(t, at(env, "elsewhere/reviewer.md")) != "outside\n" {
		t.Error("restore deleted through a symlinked parent")
	}
	if read(t, at(env, ".claude/CLAUDE.md")) == "edited\n" {
		t.Error("other paths not restored")
	}
	if !slices.Contains(opsOf(restored), "blocked") {
		t.Errorf("ops %v", opsOf(restored))
	}
}

func TestPruneKeepsFirstRun(t *testing.T) {
	env := home(t)
	s := store(env)
	path := at(env, ".claude/CLAUDE.md")
	first := take(t, s, backup.ReasonFirstRun, entry(env, claudeTool, ".claude/CLAUDE.md"))
	var later []backup.Snapshot
	for i, reason := range []backup.Reason{backup.ReasonChange, backup.ReasonChange, backup.ReasonPreRestore, backup.ReasonChange} {
		write(t, path, strings.Repeat("v", i+1)+"\n")
		later = append(later, take(t, s, reason, entry(env, claudeTool, ".claude/CLAUDE.md")))
	}

	if err := s.Prune(2); err != nil {
		t.Fatal(err)
	}

	listed, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, snap := range listed {
		ids = append(ids, snap.ID)
	}
	if want := []string{later[3].ID, later[2].ID, first.ID}; !slices.Equal(ids, want) {
		t.Errorf("kept %v, want %v", ids, want)
	}
	blobs, err := os.ReadDir(filepath.Join(env.BackupDir(), "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range blobs {
		names = append(names, b.Name())
	}
	want := []string{fileAt(t, first, path).Blob, fileAt(t, later[2], path).Blob, fileAt(t, later[3], path).Blob}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("blobs %v, want %v", names, want)
	}
}

func TestStorePermissions(t *testing.T) {
	env := home(t)
	s := store(env)
	snap := take(t, s, backup.ReasonFirstRun, entry(env, claudeTool, ".claude/skills"), entry(env, claudeTool, ".claude/CLAUDE.md"))
	checked := 0
	err := filepath.WalkDir(env.BackupDir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		want := fs.FileMode(0o600)
		if d.IsDir() {
			want = 0o700
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode %v, want %v", path, info.Mode().Perm(), want)
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !exists(t, filepath.Join(env.BackupDir(), snap.ID, "snapshot.json")) || checked < 6 {
		t.Errorf("checked %d paths", checked)
	}
}

func TestExcludedRules(t *testing.T) {
	env := paths.Env{Home: "/h"}
	for path, want := range map[string]bool{
		"/h/.codex/auth.json":                         true,
		"/h/.claude/.credentials.json":                true,
		"/h/.gemini/oauth_creds.json":                 true,
		"/h/.gemini/google_accounts.json":             true,
		"/h/.cursor/mcp-oauth-tokens-v2.json":         true,
		"/h/.gemini/mcp_oauth_tokens.json":            true,
		"/h/.local/share/opencode/mcp-auth.json":      true,
		"/h/.config/opencode/service.json":            true,
		"/h/.local/share/opencode":                    true,
		"/h/.local/share/opencode/storage/x.json":     true,
		"/h/.claude/backups":                          true,
		"/h/.claude/backups/.claude.json.backup":      true,
		"/h/.claude/skills/My-Secrets/SKILL.md":       false,
		"/h/.codex/OAuth/x":                           false,
		"/h/.claude/skills/tokenizer/SKILL.md":        false,
		"/h/.claude/skills/x/token-notes.md":          false,
		"/h/.claude/skills/x/TOKEN-NOTES.MD":          false,
		"/h/x/secrets.json":                           true,
		"/h/.codex/OAuth/client-token":                true,
		"/h/.claude/settings.json":                    false,
		"/h/.claude.json":                             false,
		"/h/.config/opencode/opencode.json":           false,
		"/h/.local/share":                             false,
		"/h/.claude/skills/backups/SKILL.md":          false,
		"/h/project/.claude/backups/x":                false,
		"/h/.config/opencode/service.json.d/settings": false,
	} {
		if got := backup.Excluded(env, path); got != want {
			t.Errorf("Excluded(%s) = %v, want %v", path, got, want)
		}
	}
	if got := backup.ExcludedPointers(env, "/h/.claude.json"); !slices.Equal(got, []string{"/customApiKeyResponses", "/oauthAccount", "/primaryApiKey"}) {
		t.Errorf("pointers %v", got)
	}
	if got := backup.ExcludedPointers(env, "/h/.claude/settings.json"); got != nil {
		t.Errorf("pointers %v", got)
	}
}

func TestLinkedClaudeStateNotReplacedOnRestore(t *testing.T) {
	env := home(t)
	s := store(env)
	path := at(env, ".claude.json")
	snap := take(t, s, backup.ReasonChange, entry(env, claudeTool, ".claude.json"))
	write(t, at(env, "dotfiles/claude.json"), `{"oauthAccount": {"accountUuid": "current-account"}}`)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dotfiles/claude.json", path); err != nil {
		t.Fatal(err)
	}

	restored := restore(t, s, snap.ID, "", false)

	if target, err := os.Readlink(path); err != nil || target != "dotfiles/claude.json" {
		t.Errorf("link replaced: %q %v", target, err)
	}
	if !slices.Equal(opsOf(restored), []string{"blocked"}) {
		t.Errorf("ops %v", opsOf(restored))
	}
}

func TestTokenNamedSkillIsBackedUp(t *testing.T) {
	env := home(t)
	s := store(env)
	write(t, at(env, ".claude/skills/token-counter/secrets.json"), `{"key": "`+credentialMarker+`"}`)
	snap := take(t, s, backup.ReasonFirstRun, entry(env, claudeTool, ".claude/skills"))

	if f := fileAt(t, snap, at(env, ".claude/skills/token-counter")); f.Kind != backup.KindDir {
		t.Errorf("token-counter kind %q", f.Kind)
	}
	skill := fileAt(t, snap, at(env, ".claude/skills/token-counter/SKILL.md"))
	if !strings.Contains(string(blob(t, env, skill)), "Count the tokens.") {
		t.Error("token-counter skill not stored")
	}
	for _, f := range snap.Files {
		if strings.HasSuffix(f.Path, "secrets.json") {
			t.Errorf("%s recorded", f.Path)
		}
	}
	assertNoMarkerStored(t, env, credentialMarker)
}

func TestChangeSnapshotOfExcludedPathFails(t *testing.T) {
	env := home(t)
	s := store(env)
	_, err := s.Take(backup.ReasonChange, []plan.SnapshotEntry{
		entry(env, claudeTool, ".claude/CLAUDE.md"),
		entry(env, codexTool, ".codex/auth.json"),
	})
	if err == nil {
		t.Fatal("change snapshot of a credential file succeeded")
	}
	if _, err := s.Snapshot("change", []plan.SnapshotEntry{entry(env, "gemini", ".gemini/oauth_creds.json")}); err == nil {
		t.Fatal("Snapshot of a credential file succeeded")
	}
	listed, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Errorf("snapshots written: %+v", listed)
	}
	for _, reason := range []backup.Reason{backup.ReasonFirstRun, backup.ReasonPreRestore} {
		snap := take(t, s, reason, entry(env, codexTool, ".codex/auth.json"))
		if !slices.Equal(snap.Skipped, []string{at(env, ".codex/auth.json")}) {
			t.Errorf("%s skipped %v", reason, snap.Skipped)
		}
	}
	assertNoMarkerStored(t, env, credentialMarker)
}

func TestRestoreProjectFileUnderRepoRoot(t *testing.T) {
	env := home(t)
	s := store(env)
	repo := filepath.Join(t.TempDir(), "repo")
	rules := filepath.Join(repo, "AGENTS.md")
	settings := filepath.Join(repo, ".claude", "settings.json")
	mcp := filepath.Join(repo, ".mcp.json")
	skill := filepath.Join(repo, ".claude", "skills", "demo", "SKILL.md")
	write(t, rules, "# Repo rules\n")
	write(t, settings, `{"model": "repo-model"}`)
	write(t, skill, "repo skill\n")
	project := func(path string) plan.SnapshotEntry {
		return plan.SnapshotEntry{Tool: claudeTool, Path: path, Root: repo}
	}
	snap := take(t, s, backup.ReasonChange, project(rules), project(settings), project(mcp), project(filepath.Join(repo, ".claude", "skills")))
	if f := fileAt(t, snap, skill); f.Root != repo {
		t.Errorf("child root %q", f.Root)
	}
	write(t, rules, "edited\n")
	write(t, settings, `{"model": "edited"}`)
	write(t, mcp, `{"mcpServers": {}}`)
	write(t, filepath.Join(repo, ".claude", "skills", "demo", "extra.md"), "added\n")

	restored := restore(t, s, snap.ID, "", false)

	if slices.Contains(opsOf(restored), "blocked") {
		t.Fatalf("blocked: %+v", restored.Actions)
	}
	if read(t, rules) != "# Repo rules\n" || read(t, settings) != `{"model": "repo-model"}` {
		t.Error("project files not restored")
	}
	if exists(t, mcp) || exists(t, filepath.Join(repo, ".claude", "skills", "demo", "extra.md")) {
		t.Error("added project files not removed")
	}
}
