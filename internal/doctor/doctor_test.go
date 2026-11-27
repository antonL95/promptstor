package doctor_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/antonL95/promptstor/internal/adapter"
	_ "github.com/antonL95/promptstor/internal/adapter/all"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/doctor"
	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/policy"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	pulledCommits  = `Pull rule "Commits"`
	moveRules      = "Move the rules of"
	deleteOriginal = "Delete the original"
	applyChanges   = "Apply these changes"
	syncNow        = "Sync now"
	handEdited     = "changed outside PromptStor"
	promoteGrant   = "Promote runtime grant"
	acknowledge    = "Acknowledge"
	editGuards     = "edit guards"
	registerMCP    = "MCP server"
	keepInSync     = "keep in sync"
	choosePrimary  = "primary"
)

type answer struct{ question, reply string }

type script struct {
	answers []answer
	asked   []string
}

func (s *script) reply(q string) (string, bool) {
	s.asked = append(s.asked, q)
	for _, a := range s.answers {
		if strings.Contains(q, a.question) {
			return a.reply, true
		}
	}
	return "", false
}

func (s *script) Select(q string, opts []string, def int) (int, error) {
	reply, found := s.reply(q)
	if !found {
		return def, nil
	}
	if at := slices.Index(opts, reply); at >= 0 {
		return at, nil
	}
	return 0, fmt.Errorf("question %q has no option %q in %v", q, reply, opts)
}

func (s *script) MultiSelect(q string, opts []string, defs []int) ([]int, error) {
	reply, found := s.reply(q)
	if !found {
		return defs, nil
	}
	var picked []int
	for _, label := range strings.Split(reply, ",") {
		at := slices.IndexFunc(opts, func(opt string) bool { return strings.HasPrefix(opt, label) })
		if at < 0 {
			return nil, fmt.Errorf("question %q has no option %q in %v", q, label, opts)
		}
		picked = append(picked, at)
	}
	return picked, nil
}

func (s *script) Confirm(q string, def bool) (bool, error) {
	reply, found := s.reply(q)
	if !found {
		return def, nil
	}
	return reply == "yes", nil
}

func (s *script) wasAsked(fragment string) bool {
	return slices.ContainsFunc(s.asked, func(q string) bool { return strings.Contains(q, fragment) })
}

func fixtureHome(t *testing.T) paths.Env {
	t.Helper()
	return testenv.Home(t, "testdata/home")
}

func newEngine(t *testing.T, env paths.Env) engine.Engine {
	t.Helper()
	cfg, _, err := config.Load(env.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	acks, err := policy.LoadAcks(env.AcksPath())
	if err != nil {
		t.Fatal(err)
	}
	return engine.Engine{
		Env:      env,
		Config:   cfg,
		Adapters: adapter.All(),
		Acks:     acks,
		Now:      func() time.Time { return time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC) },
	}
}

func run(t *testing.T, env paths.Env, p doctor.Prompter, opt doctor.Options) (string, int) {
	t.Helper()
	var out bytes.Buffer
	code, err := doctor.Run(newEngine(t, env), p, &out, opt)
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	return out.String(), code
}

func firstRun(t *testing.T, env paths.Env, answers ...answer) (string, *script) {
	t.Helper()
	s := &script{answers: append(answers, answer{applyChanges, "yes"})}
	out, _ := run(t, env, s, doctor.Options{})
	return out, s
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func libraryItems(t *testing.T, env paths.Env) []library.Item {
	t.Helper()
	items, err := library.LoadLayer(env.LibraryDir(), library.LayerPersonal, library.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func libraryItem(t *testing.T, env paths.Env, kind library.Kind, name string) (library.Item, bool) {
	t.Helper()
	items := libraryItems(t, env)
	at := slices.IndexFunc(items, func(it library.Item) bool { return it.Kind == kind && it.Name == name })
	if at < 0 {
		return library.Item{}, false
	}
	return items[at], true
}

func claudeMD(env paths.Env) string   { return filepath.Join(env.Home, ".claude", "CLAUDE.md") }
func codexMD(env paths.Env) string    { return filepath.Join(env.Home, ".codex", "AGENTS.md") }
func goStyle(env paths.Env) string    { return filepath.Join(env.Home, ".claude", "rules", "Go Style.md") }
func settings(env paths.Env) string   { return filepath.Join(env.Home, ".claude", "settings.json") }
func reviewNote(env paths.Env) string { return filepath.Join(env.Home, "notes", "review.md") }

func checkReport(t *testing.T, env paths.Env) drift.Report {
	t.Helper()
	r, err := drift.Check(newEngine(t, env), "")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func findings(r drift.Report, kind drift.FindingKind) []drift.Finding {
	return slices.DeleteFunc(slices.Clone(r.Findings), func(f drift.Finding) bool { return f.Kind != kind })
}

func TestDryRunWritesNothing(t *testing.T) {
	env := fixtureHome(t)
	before := testenv.TreeHash(t, env.Home)
	out, _ := run(t, env, &script{answers: []answer{{applyChanges, "yes"}}}, doctor.Options{DryRun: true})
	if after := testenv.TreeHash(t, env.Home); after != before {
		t.Fatalf("first run --dry-run changed the home\n%s", out)
	}
	if !strings.Contains(out, "Prefer short functions and descriptive names.") {
		t.Errorf("dry run shows no preview:\n%s", out)
	}

	firstRun(t, env)
	write(t, claudeMD(env), strings.Replace(read(t, claudeMD(env)), "before every commit", "twice", 1))
	write(t, filepath.Join(env.LibraryDir(), "permissions", "secrets.yaml"), "promptstor:\n  id: ps_0000000000000000000000DR01\ndeny_paths:\n  - ~/.ssh/**\n")
	before = testenv.TreeHash(t, env.Home)
	s := &script{answers: []answer{{handEdited, "pull into the library"}, {promoteGrant, "yes"}, {acknowledge, "yes"}, {syncNow, "yes"}}}
	out, _ = run(t, env, s, doctor.Options{DryRun: true})
	if after := testenv.TreeHash(t, env.Home); after != before {
		t.Fatalf("later run --dry-run changed the home\n%s", out)
	}
	run(t, env, &script{}, doctor.Options{DryRun: true, Check: true})
	if after := testenv.TreeHash(t, env.Home); after != before {
		t.Fatal("--check --dry-run changed the home")
	}
}

func TestFirstRunSnapshotBeforeLibraryWrite(t *testing.T) {
	t.Run("snapshot fails", func(t *testing.T) {
		env := fixtureHome(t)
		write(t, env.BackupDir(), "not a directory")
		before := testenv.TreeHash(t, env.Home)
		var out bytes.Buffer
		s := &script{answers: []answer{{applyChanges, "yes"}}}
		if _, err := doctor.Run(newEngine(t, env), s, &out, doctor.Options{}); err == nil {
			t.Fatal("doctor went on without a snapshot")
		}
		if after := testenv.TreeHash(t, env.Home); after != before {
			t.Fatal("doctor wrote before the snapshot was taken")
		}
	})
	t.Run("snapshot covers every detected tool", func(t *testing.T) {
		env := fixtureHome(t)
		firstRun(t, env, answer{keepInSync, "claude-code,codex"})
		snaps, err := backup.Store{Env: env}.List()
		if err != nil {
			t.Fatal(err)
		}
		first := slices.IndexFunc(snaps, func(s backup.Snapshot) bool { return s.Reason == backup.ReasonFirstRun })
		if first < 0 {
			t.Fatalf("no first-run snapshot in %v", snaps)
		}
		snap := snaps[first]
		for _, path := range []string{claudeMD(env), codexMD(env), filepath.Join(env.Home, ".gemini", "settings.json")} {
			if !slices.ContainsFunc(snap.Files, func(f backup.SnapFile) bool { return f.Path == path }) {
				t.Errorf("first-run snapshot lacks %s", path)
			}
		}
		if slices.ContainsFunc(snap.Files, func(f backup.SnapFile) bool { return strings.HasPrefix(f.Path, env.LibraryDir()) }) {
			t.Error("first-run snapshot holds library files, so the library was written first")
		}
		cfg, found, err := config.Load(env.ConfigFile())
		if err != nil || !found {
			t.Fatalf("config not saved: %v", err)
		}
		if cfg.FirstRunSnapshot != snap.ID {
			t.Errorf("config first_run_snapshot = %q, want %q", cfg.FirstRunSnapshot, snap.ID)
		}
		if !slices.Equal(cfg.Tools, []string{"claude-code", "codex"}) || cfg.Primary != "claude-code" {
			t.Errorf("config tools %v primary %q", cfg.Tools, cfg.Primary)
		}
	})
}

func TestFirstRunPullsSecondaryOnlyItemWhenAccepted(t *testing.T) {
	for _, accept := range []bool{true, false} {
		t.Run(fmt.Sprint(accept), func(t *testing.T) {
			env := fixtureHome(t)
			reply := "no"
			if accept {
				reply = "yes"
			}
			_, s := firstRun(t, env, answer{pulledCommits, reply})
			if !s.wasAsked(pulledCommits) {
				t.Fatalf("never asked about Commits: %v", s.asked)
			}
			if s.wasAsked(`Pull rule "Testing"`) || s.wasAsked(`Pull rule "Style"`) {
				t.Errorf("asked about items the primary holds: %v", s.asked)
			}
			it, found := libraryItem(t, env, library.KindRule, "Commits")
			if found != accept {
				t.Fatalf("Commits in library = %v, want %v", found, accept)
			}
			inClaude := strings.Contains(read(t, claudeMD(env)), "imperative mood")
			if inClaude != accept {
				t.Errorf("Commits in CLAUDE.md = %v, want %v", inClaude, accept)
			}
			if accept && (!strings.HasPrefix(string(it.ID), "ps_") || !strings.Contains(it.Body, "imperative mood")) {
				t.Errorf("pulled item %+v", it)
			}
		})
	}
}

func TestFirstRunPrimaryWinsOnConflict(t *testing.T) {
	env := fixtureHome(t)
	out, _ := firstRun(t, env)
	style, found := libraryItem(t, env, library.KindRule, "Style")
	if !found || !strings.Contains(style.Body, "Prefer short functions") {
		t.Fatalf("library Style = %+v", style)
	}
	codex := read(t, codexMD(env))
	if !strings.Contains(codex, "Prefer short functions and descriptive names.") || strings.Contains(codex, "Prefer long functions") {
		t.Errorf("codex AGENTS.md does not follow the primary:\n%s", codex)
	}
	if !strings.Contains(out, "differs") || !strings.Contains(out, "+Prefer short functions and descriptive names.") {
		t.Errorf("comparison or preview missing:\n%s", out)
	}
}

func TestAnswerNoChangesNothingButLibraryAndSnapshot(t *testing.T) {
	env := fixtureHome(t)
	var before []string
	for _, dir := range []string{".claude", ".codex", ".gemini", "notes"} {
		before = append(before, testenv.TreeHash(t, filepath.Join(env.Home, dir)))
	}
	run(t, env, &script{answers: []answer{{applyChanges, "no"}, {deleteOriginal, "yes"}}}, doctor.Options{})
	for i, dir := range []string{".claude", ".codex", ".gemini", "notes"} {
		if testenv.TreeHash(t, filepath.Join(env.Home, dir)) != before[i] {
			t.Errorf("%s changed after answering no", dir)
		}
	}
	if exists(env.ConfigFile()) {
		t.Error("config saved after answering no")
	}
	if len(libraryItems(t, env)) == 0 {
		t.Error("library not written")
	}
	state, err := os.ReadDir(env.StateDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 1 || state[0].Name() != "backups" {
		t.Errorf("state dir holds %v, want only backups", state)
	}
}

func TestFirstRunMovesHandWrittenRulesKeepingOtherText(t *testing.T) {
	env := fixtureHome(t)
	firstRun(t, env)
	doc := read(t, claudeMD(env))
	for _, kept := range []string{"# Personal instructions", "@~/notes/review.md", "<laravel-boost-guidelines>\n## Boost\nGenerated by an example tool.\n</laravel-boost-guidelines>"} {
		if !strings.Contains(doc, kept) {
			t.Errorf("CLAUDE.md lost %q:\n%s", kept, doc)
		}
	}
	if n := strings.Count(doc, "Run the full test suite before every commit."); n != 1 {
		t.Errorf("Testing appears %d times in CLAUDE.md:\n%s", n, doc)
	}
	if strings.Contains(read(t, reviewNote(env)), "Read the whole diff") {
		t.Error("imported notes/review.md still holds the moved rule")
	}
	if n := strings.Count(read(t, codexMD(env)), "Read the whole diff before commenting."); n != 1 {
		t.Errorf("Code review appears %d times in codex AGENTS.md", n)
	}
}

func TestFirstRunDeclinedMoveKeepsFileAndSkipsItsRules(t *testing.T) {
	env := fixtureHome(t)
	original := read(t, claudeMD(env))
	firstRun(t, env, answer{moveRules + " " + claudeMD(env), "no"})
	if _, found := libraryItem(t, env, library.KindRule, "Style"); found {
		t.Error("Style pulled although its file was not moved")
	}
	if doc := read(t, claudeMD(env)); !strings.HasPrefix(doc, original) {
		t.Errorf("hand-written text changed:\n%s", doc)
	}
}

func TestFirstRunDeletesRenamedOriginalOnConsent(t *testing.T) {
	for _, consent := range []bool{true, false} {
		t.Run(fmt.Sprint(consent), func(t *testing.T) {
			env := fixtureHome(t)
			reply := "no"
			if consent {
				reply = "yes"
			}
			out, s := firstRun(t, env, answer{deleteOriginal, reply})
			if !s.wasAsked(deleteOriginal) || !strings.Contains(out, goStyle(env)) {
				t.Fatalf("renamed original not listed:\n%s", out)
			}
			_, pulled := libraryItem(t, env, library.KindRule, "Go Style")
			renamed := exists(filepath.Join(env.Home, ".claude", "rules", "go-style.md"))
			if pulled != consent || renamed != consent || exists(goStyle(env)) == consent {
				t.Errorf("pulled %v, renamed %v, original kept %v", pulled, renamed, exists(goStyle(env)))
			}
		})
	}
}

func TestFirstRunAsksAboutRuleFileMovedIntoSection(t *testing.T) {
	env := fixtureHome(t)
	naming := filepath.Join(env.Home, ".claude", "rules", "Naming.md")
	write(t, naming, "## Naming\n\nUse full words in names.\n")
	_, s := firstRun(t, env, answer{deleteOriginal + " " + naming, "yes"})
	if !s.wasAsked(deleteOriginal + " " + naming) {
		t.Fatalf("not asked about %s: %v", naming, s.asked)
	}
	if exists(naming) {
		t.Error("original rule file kept after consent")
	}
	if n := strings.Count(read(t, claudeMD(env)), "Use full words in names."); n != 1 {
		t.Errorf("Naming appears %d times in CLAUDE.md", n)
	}
}

func TestFirstRunAgainAfterNoReusesLibrary(t *testing.T) {
	env := fixtureHome(t)
	run(t, env, &script{answers: []answer{{applyChanges, "no"}}}, doctor.Options{})
	ids := map[string]library.ID{}
	for _, it := range libraryItems(t, env) {
		ids[it.Name] = it.ID
	}
	firstRun(t, env)
	after := libraryItems(t, env)
	if len(after) != len(ids) {
		t.Errorf("library grew from %d to %d items", len(ids), len(after))
	}
	for _, it := range after {
		if ids[it.Name] != it.ID {
			t.Errorf("%s changed id from %s to %s", it.Name, ids[it.Name], it.ID)
		}
	}
}

func TestFirstRunStoresGuardAnswers(t *testing.T) {
	env := fixtureHome(t)
	out, s := firstRun(t, env, answer{editGuards, "no"})
	if !s.wasAsked(editGuards) || !s.wasAsked(registerMCP) {
		t.Fatalf("questions asked: %v", s.asked)
	}
	cfg, _, err := config.Load(env.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EditGuards || !cfg.RegisterMCP {
		t.Errorf("edit guards %v, register MCP %v", cfg.EditGuards, cfg.RegisterMCP)
	}
	if !strings.Contains(out, "Antigravity gets no edit guard") {
		t.Errorf("Antigravity guard note missing:\n%s", out)
	}
}

func TestSymlinkedTargetListed(t *testing.T) {
	env := fixtureHome(t)
	out, _ := firstRun(t, env)
	want := filepath.Join(env.Home, ".gemini", "GEMINI.md") + " is a symlink; PromptStor leaves it to whoever manages the link"
	if !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
	if !strings.HasPrefix(read(t, filepath.Join(env.Home, "notes", "gemini.md")), "## Testing\n\nRun the full test suite") {
		t.Error("doctor edited the file behind a symlink")
	}
}

func TestCodexHooksTrustNote(t *testing.T) {
	env := fixtureHome(t)
	var out bytes.Buffer
	opt := doctor.Options{Reconfigure: func(cfg config.Config) (engine.Engine, error) {
		e := newEngine(t, env)
		e.Config = cfg
		if cfg.EditGuards {
			e.Builtins = []library.Item{library.EditGuard(adapter.Codex, "/opt/example/promptstor")}
		}
		return e, nil
	}}
	s := &script{answers: []answer{{applyChanges, "yes"}}}
	if _, err := doctor.Run(newEngine(t, env), s, &out, opt); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Codex skips changed hooks until you trust them again in /hooks") {
		t.Errorf("codex trust note missing:\n%s", out.String())
	}
}

func TestHandEditPullUpdatesLibrary(t *testing.T) {
	env := fixtureHome(t)
	firstRun(t, env)
	write(t, claudeMD(env), strings.Replace(read(t, claudeMD(env)), "Run the full test suite before every commit.", "Run the full test suite twice.", 1))
	s := &script{answers: []answer{{handEdited, "pull into the library"}, {syncNow, "yes"}}}
	run(t, env, s, doctor.Options{})
	testing, _ := libraryItem(t, env, library.KindRule, "Testing")
	if !strings.Contains(testing.Body, "twice") {
		t.Fatalf("library Testing = %q", testing.Body)
	}
	if !strings.Contains(read(t, codexMD(env)), "Run the full test suite twice.") {
		t.Error("pulled edit not synced to codex")
	}
	if edits := findings(checkReport(t, env), drift.KindHandEdit); len(edits) > 0 {
		t.Errorf("hand edits left: %v", edits)
	}
}

func TestHandEditNeverDecidedSilently(t *testing.T) {
	env := fixtureHome(t)
	firstRun(t, env)
	edited := strings.Replace(read(t, claudeMD(env)), "Run the full test suite before every commit.", "Run the full test suite twice.", 1)
	write(t, claudeMD(env), edited)
	s := &script{}
	out, _ := run(t, env, s, doctor.Options{Yes: true})
	if !s.wasAsked(handEdited) {
		t.Fatalf("hand edit not offered: %v", s.asked)
	}
	if read(t, claudeMD(env)) != edited {
		t.Errorf("hand edit overwritten without a decision:\n%s", out)
	}
	testing, _ := libraryItem(t, env, library.KindRule, "Testing")
	if strings.Contains(testing.Body, "twice") {
		t.Error("hand edit pulled without a decision")
	}

	s = &script{answers: []answer{{handEdited, "overwrite with the library"}, {syncNow, "yes"}}}
	run(t, env, s, doctor.Options{})
	if strings.Contains(read(t, claudeMD(env)), "twice") {
		t.Error("overwrite kept the hand edit")
	}
}

func TestRuntimeGrantPromotion(t *testing.T) {
	env := fixtureHome(t)
	firstRun(t, env)
	if grants := findings(checkReport(t, env), drift.KindRuntimeGrant); len(grants) != 1 {
		t.Fatalf("runtime grants before promotion: %v", grants)
	}
	s := &script{answers: []answer{{promoteGrant, "yes"}, {syncNow, "yes"}}}
	run(t, env, s, doctor.Options{})
	var promoted []any
	for _, it := range libraryItems(t, env) {
		native, _ := it.Native[adapter.ClaudeCode].(map[string]any)
		if it.Kind == library.KindPermission && native != nil {
			promoted, _ = native["allow"].([]any)
		}
	}
	if !slices.Contains(promoted, any("Bash(git status)")) {
		t.Fatalf("no permission item allows the grant: %v", libraryItems(t, env))
	}
	if grants := findings(checkReport(t, env), drift.KindRuntimeGrant); len(grants) > 0 {
		t.Errorf("runtime grants after promotion: %v", grants)
	}
	if !strings.Contains(read(t, settings(env)), "Bash(git status)") {
		t.Error("grant removed from settings")
	}
}

func TestAcknowledgementSaved(t *testing.T) {
	env := fixtureHome(t)
	firstRun(t, env)
	write(t, filepath.Join(env.LibraryDir(), "permissions", "secrets.yaml"), "promptstor:\n  id: ps_0000000000000000000000DR01\ndeny_paths:\n  - ~/.ssh/**\n")
	acks := findings(checkReport(t, env), drift.KindAckRequired)
	if len(acks) == 0 {
		t.Fatal("fixture needs an acknowledgement")
	}
	s := &script{answers: []answer{{acknowledge, "yes"}, {syncNow, "yes"}}}
	run(t, env, s, doctor.Options{})
	saved, err := policy.LoadAcks(env.AcksPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.List) != len(acks) || saved.List[0].ItemID != "ps_0000000000000000000000DR01" || saved.List[0].Hash == "" {
		t.Fatalf("acks %+v, want one per %v", saved.List, acks)
	}
	if left := findings(checkReport(t, env), drift.KindAckRequired); len(left) > 0 {
		t.Errorf("still to acknowledge: %v", left)
	}
}

type notifier struct{ reports []drift.Report }

func (n *notifier) Notify(_ paths.Env, r drift.Report) error {
	n.reports = append(n.reports, r)
	return nil
}

func TestCheckExitCodes(t *testing.T) {
	env := fixtureHome(t)
	var out bytes.Buffer
	if code, err := doctor.Run(newEngine(t, env), &script{}, &out, doctor.Options{Check: true}); code != 2 || err == nil {
		t.Errorf("check before the first run: code %d, err %v", code, err)
	}
	firstRun(t, env)
	n := &notifier{}
	if _, code := run(t, env, &script{}, doctor.Options{Check: true, Notify: true, Notifier: n}); code != 0 {
		t.Errorf("clean check exit %d", code)
	}
	write(t, claudeMD(env), strings.Replace(read(t, claudeMD(env)), "before every commit", "twice", 1))
	s := &script{}
	out2, code := run(t, env, s, doctor.Options{Check: true, Notify: true, Notifier: n})
	if code != 1 {
		t.Errorf("drift check exit %d:\n%s", code, out2)
	}
	if len(s.asked) > 0 {
		t.Errorf("--check asked %v", s.asked)
	}
	last, found, err := drift.ReadLastCheck(env)
	if err != nil || !found || last.Clean {
		t.Errorf("last check %+v found %v err %v", last, found, err)
	}
	if len(n.reports) != 2 || n.reports[1].Clean() {
		t.Errorf("notifier saw %d reports", len(n.reports))
	}
}

func TestNoTTYWithoutYesFails(t *testing.T) {
	var out bytes.Buffer
	p := doctor.LinePrompter(strings.NewReader("1\n"), &out, false, false)
	if _, err := p.Select("Which tool?", []string{"claude-code", "codex"}, 0); err == nil || err.Error() != "doctor needs a terminal or --yes" {
		t.Errorf("select without a terminal: %v", err)
	}
	if _, err := p.Confirm("Apply?", true); err == nil {
		t.Error("confirm without a terminal succeeded")
	}
	env := fixtureHome(t)
	before := testenv.TreeHash(t, env.Home)
	if _, err := doctor.Run(newEngine(t, env), p, &out, doctor.Options{}); err == nil {
		t.Error("doctor ran without a terminal or --yes")
	}
	if testenv.TreeHash(t, env.Home) != before {
		t.Error("doctor wrote before failing")
	}
	yes := doctor.LinePrompter(strings.NewReader(""), &out, false, true)
	if got, err := yes.Select("Which tool?", []string{"claude-code", "codex"}, 1); err != nil || got != 1 {
		t.Errorf("--yes select = %d, %v", got, err)
	}
}

func TestLinePrompterAcceptsNumberOrLabel(t *testing.T) {
	var out bytes.Buffer
	p := doctor.LinePrompter(strings.NewReader("2\ncodex\n\nnope\n1\ny\n1,3\n"), &out, true, false)
	for _, want := range []int{1, 1, 0} {
		got, err := p.Select("Which tool?", []string{"claude-code", "codex"}, 0)
		if err != nil || got != want {
			t.Fatalf("select = %d, %v; want %d", got, err, want)
		}
	}
	if got, err := p.Select("Which tool?", []string{"claude-code", "codex"}, 1); err != nil || got != 0 {
		t.Errorf("select after a bad answer = %d, %v", got, err)
	}
	if ok, err := p.Confirm("Apply?", false); err != nil || !ok {
		t.Errorf("confirm = %v, %v", ok, err)
	}
	if got, err := p.MultiSelect("Tools?", []string{"a", "b", "c"}, nil); err != nil || !slices.Equal(got, []int{0, 2}) {
		t.Errorf("multi select = %v, %v", got, err)
	}
	if !strings.Contains(out.String(), "  1) claude-code\n  2) codex\n") {
		t.Errorf("options not numbered:\n%s", out.String())
	}
	if _, err := p.Confirm("Again?", false); !errors.Is(err, doctor.ErrNoAnswer) {
		t.Errorf("confirm at end of input: %v", err)
	}
}

func TestForeignImporterPluginsWarned(t *testing.T) {
	env := fixtureHome(t)
	write(t, filepath.Join(env.Home, ".gemini", "antigravity", "settings.json"), "{}\n")
	write(t, filepath.Join(env.Home, ".gemini", "config", "plugins", "example-plugin", "plugin.json"), "{}\n")
	out, _ := run(t, env, &script{}, doctor.Options{DryRun: true, Yes: true})
	if !strings.Contains(out, "Antigravity imported plugins may duplicate PromptStor items and hold secrets in plain text") {
		t.Errorf("plugin warning missing:\n%s", out)
	}
}
