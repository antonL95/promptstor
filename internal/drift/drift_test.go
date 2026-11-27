package drift_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/policy"
	"github.com/antonL95/promptstor/internal/testenv"
)

const (
	ruleID       library.ID = "ps_0000000000000000000000DRF1"
	skillID      library.ID = "ps_0000000000000000000000DRF2"
	serverID     library.ID = "ps_0000000000000000000000DRF3"
	permissionID library.ID = "ps_0000000000000000000000DRF4"
	secretValue             = "PSFIXTURE-DRIFT-SECRET-7Q2K"
)

var clockStart = time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

type resolver map[string]string

func (r resolver) Resolve(ref string) (string, error) {
	if value, found := r[ref]; found {
		return value, nil
	}
	return "", errors.New("no value")
}

func home(t *testing.T) paths.Env {
	t.Helper()
	return testenv.Home(t, "testdata/home")
}

func newEngine(env paths.Env, tools ...string) engine.Engine {
	clock := clockStart
	return engine.Engine{
		Env:      env,
		Config:   config.Config{Version: 1, Tools: tools},
		Adapters: adapter.All(),
		Resolver: resolver{},
		Now: func() time.Time {
			clock = clock.Add(time.Second)
			return clock
		},
	}
}

func at(env paths.Env, rel string) string {
	return filepath.Join(env.Home, filepath.FromSlash(rel))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func replaceIn(t *testing.T, path, old, replacement string) {
	t.Helper()
	content := readFile(t, path)
	if !strings.Contains(content, old) {
		t.Fatalf("%s lacks %q:\n%s", path, old, content)
	}
	writeFile(t, path, strings.Replace(content, old, replacement, 1))
}

func importUser(t *testing.T, env paths.Env, tools ...string) map[string]adapter.Imported {
	t.Helper()
	imports := map[string]adapter.Imported{}
	for _, tool := range tools {
		a, found := adapter.Get(tool)
		if !found {
			t.Fatalf("no adapter %s", tool)
		}
		imported, err := a.Import(adapter.Target{Env: env, Scope: adapter.ScopeUser, Selected: tools})
		if err != nil {
			t.Fatal(err)
		}
		imports[tool] = imported
	}
	return imports
}

func mustSync(t *testing.T, e engine.Engine, projectRoot string) plan.ApplyResult {
	t.Helper()
	result, p, err := e.Sync(projectRoot, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Changes {
		if c.Blocked != "" {
			t.Fatalf("%s blocked: %s", c.Path, c.Blocked)
		}
	}
	return result
}

func mustCheck(t *testing.T, e engine.Engine, projectRoot string) drift.Report {
	t.Helper()
	r, err := drift.Check(e, projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func ofKind(findings []drift.Finding, kind drift.FindingKind) []drift.Finding {
	var matched []drift.Finding
	for _, f := range findings {
		if f.Kind == kind {
			matched = append(matched, f)
		}
	}
	return matched
}

func named(findings []drift.Finding, name string) []drift.Finding {
	var matched []drift.Finding
	for _, f := range findings {
		if f.Name == name {
			matched = append(matched, f)
		}
	}
	return matched
}

func dump(findings []drift.Finding) string {
	var b strings.Builder
	for _, f := range findings {
		b.WriteString(string(f.Kind) + " | " + f.Tool + " | " + string(f.Item) + " | " + f.Name + " | " + f.Path + " | " + f.Detail + "\n")
	}
	return b.String()
}

func TestCompareToolsFindsStaleCodexCopy(t *testing.T) {
	env := home(t)

	findings := drift.CompareTools(importUser(t, env, adapter.ClaudeCode, adapter.Codex))

	differs := ofKind(findings, drift.KindDiffers)
	if len(differs) != 1 {
		t.Fatalf("want one differs finding:\n%s", dump(findings))
	}
	f := differs[0]
	if f.Name != "Style" || f.Tool != adapter.Codex || f.Path != at(env, ".codex/AGENTS.md") {
		t.Errorf("differs %+v", f)
	}
	if !strings.Contains(f.Detail, adapter.ClaudeCode) || !strings.Contains(f.Detail, at(env, ".claude/CLAUDE.md")) {
		t.Errorf("detail %q does not name the copy it differs from", f.Detail)
	}
	if got := ofKind(findings, drift.KindOnlyIn); len(got) != 0 {
		t.Errorf("only-in findings although both tools hold every section:\n%s", dump(got))
	}
}

func TestGeminiTildeImportsShowAsUnresolvedAndMissingRules(t *testing.T) {
	env := home(t)

	findings := drift.CompareTools(importUser(t, env, adapter.ClaudeCode, adapter.GeminiCLI))

	unresolved := ofKind(findings, drift.KindUnresolvedImport)
	reasons := map[string]bool{}
	for _, f := range unresolved {
		if f.Tool != adapter.GeminiCLI || f.Path != at(env, ".gemini/GEMINI.md") {
			t.Errorf("unresolved %+v", f)
		}
		for _, reason := range []string{"tilde", "syntax"} {
			if strings.Contains(f.Detail, reason) {
				reasons[reason] = true
			}
		}
	}
	if len(unresolved) != 2 || !reasons["tilde"] || !reasons["syntax"] {
		t.Errorf("want a tilde and a syntax unresolved import:\n%s", dump(unresolved))
	}
	if slices.ContainsFunc(unresolved, func(f drift.Finding) bool { return !strings.Contains(f.Detail, "line ") }) {
		t.Errorf("unresolved findings lack the line:\n%s", dump(unresolved))
	}
	review := named(ofKind(findings, drift.KindOnlyIn), "Code review")
	if len(review) != 1 || review[0].Tool != adapter.ClaudeCode || !strings.Contains(review[0].Detail, adapter.GeminiCLI) {
		t.Errorf("want Code review only in claude-code:\n%s", dump(findings))
	}
	if got := named(findings, "Testing"); len(got) != 0 {
		t.Errorf("Testing is identical in both tools:\n%s", dump(got))
	}
}

func TestGeminiUnresolvedWhileAntigravityResolves(t *testing.T) {
	env := home(t)

	findings := drift.CompareTools(importUser(t, env, adapter.Antigravity, adapter.GeminiCLI))

	for _, f := range ofKind(findings, drift.KindUnresolvedImport) {
		if f.Tool != adapter.GeminiCLI {
			t.Errorf("antigravity resolves its imports: %+v", f)
		}
	}
	if len(ofKind(findings, drift.KindUnresolvedImport)) == 0 {
		t.Error("gemini-cli unresolved imports missing")
	}
	review := named(ofKind(findings, drift.KindOnlyIn), "Code review")
	if len(review) != 1 || review[0].Tool != adapter.Antigravity {
		t.Errorf("want Code review only in antigravity:\n%s", dump(findings))
	}
	if got := ofKind(findings, drift.KindDiffers); len(got) != 0 {
		t.Errorf("both tools read the same GEMINI.md:\n%s", dump(got))
	}
}

func rule(name, body, source string) library.Item {
	return library.Item{Kind: library.KindRule, Name: name, Body: body, Source: source}
}

func TestCompareSameSourceFileIsNotDrift(t *testing.T) {
	imports := map[string]adapter.Imported{
		adapter.ClaudeCode: {Items: []library.Item{rule("Testing", "Run the tests.", "/fixture/project/AGENTS.md")}},
		adapter.AgentsMD:   {Items: []library.Item{rule("Testing", "Run the tests.\n\nBoost block stripped.", "/fixture/project/AGENTS.md")}},
	}

	if got := drift.CompareTools(imports); len(got) != 0 {
		t.Errorf("two views of one file reported:\n%s", dump(got))
	}
}

func TestCompareOnlyIn(t *testing.T) {
	imports := map[string]adapter.Imported{
		adapter.ClaudeCode: {Items: []library.Item{
			rule("Testing", "Run the tests.", "/fixture/home/.claude/CLAUDE.md"),
			rule("Commits", "Write short subjects.", "/fixture/home/.claude/CLAUDE.md"),
			{Kind: library.KindSkill, Name: "release-notes", Body: "Summarise.", Source: "/fixture/home/.claude/skills/release-notes/SKILL.md"},
		}},
		adapter.Codex: {Items: []library.Item{
			rule("  testing ", "Run the tests.\n", "/fixture/home/.codex/AGENTS.md"),
		}},
		adapter.AgentsMD: {Items: []library.Item{
			rule("Testing", "Run the tests.", "/fixture/project/AGENTS.md"),
		}},
	}

	findings := drift.CompareTools(imports)

	want := []drift.Finding{
		{Kind: drift.KindOnlyIn, Tool: adapter.ClaudeCode, Name: "Commits", Path: "/fixture/home/.claude/CLAUDE.md", Detail: "rule not in agents-md, codex"},
		{Kind: drift.KindOnlyIn, Tool: adapter.ClaudeCode, Name: "release-notes", Path: "/fixture/home/.claude/skills/release-notes/SKILL.md", Detail: "skill not in codex"},
	}
	if !slices.Equal(findings, want) {
		t.Errorf("got\n%swant\n%s", dump(findings), dump(want))
	}
}

func TestComparePreambleContentCountsAsRules(t *testing.T) {
	env := home(t)
	writeFile(t, at(env, ".claude/CLAUDE.md"), "@~/notes/review.md\n")
	writeFile(t, at(env, ".codex/AGENTS.md"), "Read the whole diff before commenting.\n")

	findings := drift.CompareTools(importUser(t, env, adapter.ClaudeCode, adapter.Codex))

	if len(findings) != 0 {
		t.Errorf("preamble with the same text as a headed section reported:\n%s", dump(findings))
	}

	writeFile(t, at(env, ".codex/AGENTS.md"), "Read only the changed lines.\n")
	findings = drift.CompareTools(importUser(t, env, adapter.ClaudeCode, adapter.Codex))

	preamble := named(ofKind(findings, drift.KindOnlyIn), "AGENTS")
	if len(preamble) != 1 || preamble[0].Tool != adapter.Codex || preamble[0].Path != at(env, ".codex/AGENTS.md") {
		t.Errorf("changed preamble hidden:\n%s", dump(findings))
	}
}

func TestCompareHeadinglessFileDiffers(t *testing.T) {
	imports := map[string]adapter.Imported{
		adapter.ClaudeCode: {Items: []library.Item{
			rule("style", "Prefer short functions.", "/fixture/home/.claude/rules/style.md"),
			rule("review", "Read the whole diff.", "/fixture/home/.claude/rules/review.md"),
		}},
		adapter.Cursor: {Items: []library.Item{
			rule("style", "Prefer long functions.", "/fixture/home/.cursor/rules/style.mdc"),
			rule("checklist", "Read the whole diff.", "/fixture/home/.cursor/rules/checklist.mdc"),
			rule("empty", " \n\t\n", "/fixture/home/.cursor/rules/empty.mdc"),
		}},
	}

	findings := drift.CompareTools(imports)

	want := []drift.Finding{{
		Kind:   drift.KindDiffers,
		Tool:   adapter.Cursor,
		Name:   "style",
		Path:   "/fixture/home/.cursor/rules/style.mdc",
		Detail: "differs from claude-code (/fixture/home/.claude/rules/style.md)",
	}}
	if !slices.Equal(findings, want) {
		t.Errorf("got\n%swant\n%s", dump(findings), dump(want))
	}
}

func TestCompareToolsIsDeterministic(t *testing.T) {
	env := home(t)
	imports := importUser(t, env, adapter.ClaudeCode, adapter.Codex, adapter.GeminiCLI, adapter.Antigravity)

	first := drift.CompareTools(imports)
	for range 5 {
		if got := drift.CompareTools(imports); !slices.Equal(got, first) {
			t.Fatalf("order changed:\n%s\nvs\n%s", dump(got), dump(first))
		}
	}
}

func TestCheckCleanAfterSync(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode, adapter.GeminiCLI)
	mustSync(t, e, "")

	r := mustCheck(t, e, "")

	if !r.Clean() {
		t.Errorf("not clean after sync:\n%s", dump(r.Findings))
	}
	for _, kind := range []drift.FindingKind{drift.KindHandEdit, drift.KindPending, drift.KindMissing, drift.KindAckRequired} {
		if got := ofKind(r.Findings, kind); len(got) != 0 {
			t.Errorf("%s after sync:\n%s", kind, dump(got))
		}
	}
	p, err := e.Plan("", plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.LibraryHash != p.LibraryHash || r.LibraryHash == "" {
		t.Errorf("library hash %q, plan %q", r.LibraryHash, p.LibraryHash)
	}
	if r.Generated.IsZero() || !r.Generated.After(clockStart) {
		t.Errorf("generated %v not from the engine clock", r.Generated)
	}
}

func TestCheckBeforeSyncIsPending(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode)

	r := mustCheck(t, e, "")

	if r.Clean() || len(ofKind(r.Findings, drift.KindPending)) == 0 {
		t.Errorf("nothing pending before the first sync:\n%s", dump(r.Findings))
	}
}

func TestCheckWritesNothing(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode, adapter.GeminiCLI)
	mustSync(t, e, "")
	replaceIn(t, at(env, ".claude/CLAUDE.md"), "Write in plain language.", "Write tersely.")
	before := testenv.TreeHash(t, env.Home)

	mustCheck(t, e, "")

	if testenv.TreeHash(t, env.Home) != before {
		t.Error("check changed the home or state")
	}
}

func TestCheckHandEdit(t *testing.T) {
	cases := []struct {
		name   string
		tools  []string
		edit   func(t *testing.T, env paths.Env, project string)
		tool   string
		path   func(env paths.Env, project string) string
		detail string
	}{
		{
			name:  "managed section",
			tools: []string{adapter.ClaudeCode},
			edit: func(t *testing.T, env paths.Env, _ string) {
				replaceIn(t, at(env, ".claude/CLAUDE.md"), "Write in plain language.", "Write tersely.")
			},
			tool:   adapter.ClaudeCode,
			path:   func(env paths.Env, _ string) string { return at(env, ".claude/CLAUDE.md") },
			detail: "changed outside promptstor",
		},
		{
			name:  "gemini memory write",
			tools: []string{adapter.GeminiCLI},
			edit: func(t *testing.T, env paths.Env, _ string) {
				replaceIn(t, at(env, ".gemini/GEMINI.md"), "Write in plain language.", "Write in plain language.\n- The user prefers tabs.")
			},
			tool:   adapter.GeminiCLI,
			path:   func(env paths.Env, _ string) string { return at(env, ".gemini/GEMINI.md") },
			detail: "agent memory write",
		},
		{
			name:  "owned settings key",
			tools: []string{adapter.GeminiCLI},
			edit: func(t *testing.T, _ paths.Env, project string) {
				path := filepath.Join(project, ".gemini", "settings.json")
				content := readFile(t, path)
				var doc map[string]any
				if err := json.Unmarshal([]byte(content), &doc); err != nil {
					t.Fatal(err)
				}
				doc["context"] = map[string]any{"fileName": []any{"CONTEXT.md"}}
				data, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, path, string(data))
			},
			tool:   adapter.GeminiCLI,
			path:   func(_ paths.Env, project string) string { return filepath.Join(project, ".gemini", "settings.json") },
			detail: "/context/fileName",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := home(t)
			project := t.TempDir()
			writeFile(t, filepath.Join(project, ".promptstor", "rules", "project-rule.md"), "---\nname: Project rule\npromptstor:\n  id: ps_0000000000000000000000PRJ1\n---\nProject wording.\n")
			e := newEngine(env, tc.tools...)
			mustSync(t, e, project)
			tc.edit(t, env, project)
			before := testenv.TreeHash(t, project)

			r := mustCheck(t, e, project)

			edits := ofKind(r.Findings, drift.KindHandEdit)
			if len(edits) != 1 {
				t.Fatalf("want one hand edit:\n%s", dump(r.Findings))
			}
			f := edits[0]
			if f.Tool != tc.tool || f.Path != tc.path(env, project) || !strings.Contains(f.Detail, tc.detail) || f.Item == "" {
				t.Errorf("hand edit %+v", f)
			}
			if r.Clean() {
				t.Error("report with a hand edit is clean")
			}
			if len(ofKind(r.Findings, drift.KindPending)) != 0 {
				t.Errorf("hand edit also reported as pending:\n%s", dump(r.Findings))
			}
			if testenv.TreeHash(t, project) != before {
				t.Error("check overwrote the hand edit")
			}
		})
	}
}

func TestCheckPendingAndMissing(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode)
	mustSync(t, e, "")
	replaceIn(t, filepath.Join(env.LibraryDir(), "rules", "plain-language.md"), "Write in plain language.", "Write in plain, short language.")
	skill := at(env, ".claude/skills/release-notes/SKILL.md")
	if err := os.Remove(skill); err != nil {
		t.Fatal(err)
	}

	r := mustCheck(t, e, "")

	pending := ofKind(r.Findings, drift.KindPending)
	if len(pending) != 1 || pending[0].Path != at(env, ".claude/CLAUDE.md") || pending[0].Item != ruleID {
		t.Errorf("pending:\n%s", dump(r.Findings))
	}
	missing := ofKind(r.Findings, drift.KindMissing)
	if len(missing) != 1 || missing[0].Path != skill || missing[0].Item != skillID {
		t.Errorf("missing:\n%s", dump(r.Findings))
	}
	if len(ofKind(r.Findings, drift.KindHandEdit)) != 0 {
		t.Errorf("library change reported as hand edit:\n%s", dump(r.Findings))
	}
}

func TestCheckReformatIsNotHandEdit(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.GeminiCLI)
	mustSync(t, e, "")
	path := at(env, ".gemini/settings.json")
	synced := readFile(t, path)
	if !strings.Contains(synced, "mcpServers") || !strings.Contains(synced, "\t\"ui\"") {
		t.Fatalf("sync did not write the server into the tab indented file:\n%s", synced)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(synced), &doc); err != nil {
		t.Fatal(err)
	}
	rewritten, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(rewritten)+"\n")
	if strings.Index(string(rewritten), `"general"`) > strings.Index(string(rewritten), `"ui"`) == (strings.Index(synced, `"general"`) > strings.Index(synced, `"ui"`)) {
		t.Fatal("rewrite did not reorder the keys")
	}

	r := mustCheck(t, e, "")

	if got := ofKind(r.Findings, drift.KindHandEdit); len(got) != 0 {
		t.Errorf("reformat reported as hand edit:\n%s", dump(got))
	}
	if !r.Clean() {
		t.Errorf("reformatted file not clean:\n%s", dump(r.Findings))
	}
}

func TestCheckAfterRestoreHasNoMissing(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode, adapter.GeminiCLI)
	first := mustSync(t, e, "")
	if _, err := e.Restore(first.SnapshotID, "", false); err != nil {
		t.Fatal(err)
	}

	r := mustCheck(t, e, "")

	if got := ofKind(r.Findings, drift.KindMissing); len(got) != 0 {
		t.Errorf("missing after restore:\n%s", dump(got))
	}
	if got := ofKind(r.Findings, drift.KindHandEdit); len(got) != 0 {
		t.Errorf("hand edit after restore:\n%s", dump(got))
	}
	if len(ofKind(r.Findings, drift.KindPending)) == 0 {
		t.Errorf("restored files not pending:\n%s", dump(r.Findings))
	}
}

func addPermission(t *testing.T, env paths.Env, body string) {
	t.Helper()
	writeFile(t, filepath.Join(env.LibraryDir(), "permissions", "guard.yaml"), "promptstor:\n  id: "+string(permissionID)+"\ndescription: Synthetic guard\n"+body)
}

func permissionItem(t *testing.T, e engine.Engine) library.Item {
	t.Helper()
	merged, _, err := e.Library("")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range merged.Items {
		if it.ID == permissionID {
			return it
		}
	}
	t.Fatal("permission not in library")
	return library.Item{}
}

func TestAcknowledgedReportedEveryTime(t *testing.T) {
	env := home(t)
	addPermission(t, env, "deny_commands:\n  - git push --force\n")
	e := newEngine(env, adapter.Cursor)

	r := mustCheck(t, e, "")

	required := ofKind(r.Findings, drift.KindAckRequired)
	if len(required) != 1 || required[0].Item != permissionID || required[0].Tool != adapter.Cursor {
		t.Fatalf("ack required:\n%s", dump(r.Findings))
	}
	if r.Clean() {
		t.Error("unacknowledged block is clean")
	}

	e.Acks.Add(policy.Ack{ItemID: permissionID, Tool: adapter.Cursor, Hash: library.ContentHash(permissionItem(t, e))})
	mustSync(t, e, "")
	for run := range 2 {
		r = mustCheck(t, e, "")
		if got := ofKind(r.Findings, drift.KindAckRequired); len(got) != 0 {
			t.Errorf("run %d: still ack required:\n%s", run, dump(got))
		}
		acknowledged := slices.ContainsFunc(ofKind(r.Findings, drift.KindUnsupported), func(f drift.Finding) bool {
			return f.Item == permissionID && f.Tool == adapter.Cursor && strings.HasPrefix(f.Detail, "acknowledged: ")
		})
		if !acknowledged {
			t.Errorf("run %d: acknowledged item not reported:\n%s", run, dump(r.Findings))
		}
	}
}

func TestCheckRuntimeGrantsExcludeWrittenEntries(t *testing.T) {
	env := home(t)
	addPermission(t, env, "allow_mcp_servers:\n  - example-docs\n")
	e := newEngine(env, adapter.ClaudeCode)
	mustSync(t, e, "")
	if !strings.Contains(readFile(t, at(env, ".claude/settings.json")), "mcp__example-docs") {
		t.Fatal("sync did not write the allow rule")
	}

	r := mustCheck(t, e, "")

	grants := ofKind(r.Findings, drift.KindRuntimeGrant)
	if len(grants) != 1 || grants[0].Name != "Bash(git status)" || grants[0].Tool != adapter.ClaudeCode || grants[0].Path != at(env, ".claude/settings.json") {
		t.Errorf("runtime grants:\n%s", dump(r.Findings))
	}
}

func TestCheckReportsImportFindings(t *testing.T) {
	env := home(t)
	writeFile(t, at(env, ".claude/CLAUDE.md"), readFile(t, at(env, ".claude/CLAUDE.md"))+"\n<laravel-boost-guidelines>\n## Generated\n\nGenerated text.\n</laravel-boost-guidelines>\n")
	e := newEngine(env, adapter.Antigravity, adapter.ClaudeCode, adapter.GeminiCLI)
	mustSync(t, e, "")

	r := mustCheck(t, e, "")

	unresolved := ofKind(r.Findings, drift.KindUnresolvedImport)
	if len(unresolved) == 0 || slices.ContainsFunc(unresolved, func(f drift.Finding) bool { return f.Tool != adapter.GeminiCLI }) {
		t.Errorf("unresolved imports:\n%s", dump(unresolved))
	}
	boost := slices.ContainsFunc(ofKind(r.Findings, drift.KindForeign), func(f drift.Finding) bool {
		return f.Tool == adapter.ClaudeCode && f.Name == "laravel-boost" && f.Path == at(env, ".claude/CLAUDE.md")
	})
	if !boost {
		t.Errorf("foreign:\n%s", dump(r.Findings))
	}
	if !slices.ContainsFunc(r.Findings, func(f drift.Finding) bool {
		return f.Tool == adapter.Antigravity && f.Path == at(env, ".gemini/GEMINI.md") && strings.Contains(f.Detail, "shared with Gemini CLI")
	}) {
		t.Errorf("antigravity note missing:\n%s", dump(r.Findings))
	}
	if !r.Clean() {
		t.Errorf("import findings made the report unclean:\n%s", dump(r.Findings))
	}
}

func TestLoadedTwiceInfo(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode, adapter.Codex, adapter.Cursor, adapter.OpenCode)

	r := mustCheck(t, e, "")

	info := ofKind(r.Findings, drift.KindInfo)
	var homes []string
	for _, f := range info {
		if strings.HasPrefix(f.Detail, "loaded twice by ") {
			homes = append(homes, f.Path)
			if f.Detail != "loaded twice by cursor, opencode" {
				t.Errorf("detail %q", f.Detail)
			}
		}
	}
	want := []string{at(env, ".agents/skills"), at(env, ".claude/skills")}
	if !slices.Equal(homes, want) {
		t.Errorf("homes %v, want %v:\n%s", homes, want, dump(info))
	}

	single := mustCheck(t, newEngine(env, adapter.ClaudeCode, adapter.Codex), "")
	if slices.ContainsFunc(single.Findings, func(f drift.Finding) bool { return strings.HasPrefix(f.Detail, "loaded twice") }) {
		t.Errorf("no selected tool reads two homes:\n%s", dump(single.Findings))
	}
}

func TestCheckReportsBlockedAndViolations(t *testing.T) {
	env := home(t)
	elsewhere := filepath.Join(t.TempDir(), "CLAUDE.md")
	writeFile(t, elsewhere, "# Elsewhere\n")
	claudeMD := at(env, ".claude/CLAUDE.md")
	if err := os.Remove(claudeMD); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, claudeMD); err != nil {
		t.Fatal(err)
	}
	e := newEngine(env, adapter.ClaudeCode)
	e.Builtins = []library.Item{{ID: ruleID, Kind: library.KindRule, Name: "Plain language", Body: "Company wording.", Locked: true}}

	r := mustCheck(t, e, "")

	symlinked := ofKind(r.Findings, drift.KindSymlinked)
	if len(symlinked) != 1 || symlinked[0].Path != claudeMD || !strings.Contains(symlinked[0].Detail, elsewhere) {
		t.Errorf("symlinked:\n%s", dump(r.Findings))
	}
	violations := ofKind(r.Findings, drift.KindLockViolation)
	if len(violations) != 1 || violations[0].Item != ruleID {
		t.Errorf("lock violations:\n%s", dump(r.Findings))
	}
}

func TestCheckSecretNeverShown(t *testing.T) {
	env := home(t)
	writeFile(t, filepath.Join(env.LibraryDir(), "mcp", "example-docs.yaml"), "promptstor:\n  id: "+string(serverID)+"\ntransport: stdio\ncommand: example-docs-server\nenv:\n  EXAMPLE_TOKEN:\n    ref: keychain:example-service/example-account\n")
	e := newEngine(env, adapter.ClaudeCode)
	e.Config.ResolveSecrets = []string{"example-docs"}
	e.Resolver = resolver{"keychain:example-service/example-account": secretValue}
	mustSync(t, e, "")
	replaceIn(t, at(env, ".claude.json"), secretValue, secretValue+"-EDITED")

	r := mustCheck(t, e, "")

	edits := ofKind(r.Findings, drift.KindHandEdit)
	if len(edits) != 1 || edits[0].Item != serverID || !strings.Contains(edits[0].Detail, "(secret, not shown)") {
		t.Fatalf("secret hand edit:\n%s", dump(r.Findings))
	}
	var text, encoded bytes.Buffer
	drift.RenderText(&text, r)
	if err := drift.RenderJSON(&encoded, r); err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{"text": text.String(), "json": encoded.String()} {
		if strings.Contains(out, secretValue) {
			t.Errorf("%s output shows the secret", name)
		}
		if !strings.Contains(out, "(secret, not shown)") {
			t.Errorf("%s output lacks the secret marker", name)
		}
	}
}

func sampleReport() drift.Report {
	return drift.Report{
		Generated:   time.Date(2026, 11, 27, 9, 30, 0, 0, time.UTC),
		LibraryHash: "0f3c9a",
		Findings: []drift.Finding{
			{Kind: drift.KindInfo, Path: "/fixture/home/.agents/skills", Detail: "loaded twice by cursor, opencode"},
			{Kind: drift.KindRuntimeGrant, Tool: adapter.ClaudeCode, Name: "Bash(git status)", Path: "/fixture/home/.claude/settings.json", Detail: "/permissions/allow@=Bash(git status)"},
			{Kind: drift.KindHandEdit, Tool: adapter.GeminiCLI, Item: ruleID, Path: "/fixture/home/.gemini/GEMINI.md", Detail: "agent memory write (Plain language)"},
			{Kind: drift.KindUnsupported, Tool: adapter.Codex, Item: permissionID, Name: "guard", Detail: "acknowledged: deny commands cannot be expressed"},
			{Kind: drift.KindAckRequired, Tool: adapter.Cursor, Item: permissionID, Name: "guard", Detail: "deny commands cannot be expressed"},
			{Kind: drift.KindPending, Tool: adapter.ClaudeCode, Item: ruleID, Path: "/fixture/home/.claude/CLAUDE.md", Detail: "library changed (Plain language)"},
		},
	}
}

func TestRenderJSONGolden(t *testing.T) {
	var out bytes.Buffer

	if err := drift.RenderJSON(&out, sampleReport()); err != nil {
		t.Fatal(err)
	}

	testenv.Golden(t, "testdata/golden/report.json", out.Bytes())
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
}

func TestRenderTextGolden(t *testing.T) {
	var out bytes.Buffer

	drift.RenderText(&out, sampleReport())

	testenv.Golden(t, "testdata/golden/report.txt", out.Bytes())
}

func TestRenderTextClean(t *testing.T) {
	var out bytes.Buffer

	drift.RenderText(&out, drift.Report{Generated: clockStart})

	if out.String() != "Everything is in sync.\n" {
		t.Errorf("got %q", out.String())
	}
}

func TestCleanKinds(t *testing.T) {
	blocking := []drift.FindingKind{drift.KindDiffers, drift.KindMissing, drift.KindPending, drift.KindHandEdit, drift.KindAckRequired, drift.KindSharedConflict}
	for _, kind := range drift.Kinds() {
		r := drift.Report{Findings: []drift.Finding{{Kind: kind}}}
		if got, want := r.Clean(), !slices.Contains(blocking, kind); got != want {
			t.Errorf("%s: clean %v, want %v", kind, got, want)
		}
	}
	if len(drift.Kinds()) != 17 {
		t.Errorf("%d kinds", len(drift.Kinds()))
	}
}

func TestFingerprintIgnoresGeneratedAndOrder(t *testing.T) {
	a := sampleReport()
	b := sampleReport()
	b.Generated = b.Generated.Add(time.Hour)
	slices.Reverse(b.Findings)

	if a.Fingerprint() != b.Fingerprint() {
		t.Error("fingerprint depends on time or order")
	}
	b.Findings = b.Findings[1:]
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("fingerprint ignores findings")
	}
	if len(a.Fingerprint()) != 64 {
		t.Errorf("fingerprint %q is not a sha256", a.Fingerprint())
	}
}

func TestLastCheckRoundTrip(t *testing.T) {
	env := home(t)
	r := sampleReport()

	if _, found, err := drift.ReadLastCheck(env); err != nil || found {
		t.Fatalf("found %v err %v before any check", found, err)
	}
	if err := drift.WriteLastCheck(env, r, clockStart); err != nil {
		t.Fatal(err)
	}
	got, found, err := drift.ReadLastCheck(env)
	if err != nil || !found {
		t.Fatalf("found %v err %v", found, err)
	}

	if !got.Checked.Equal(clockStart) || got.LibraryHash != r.LibraryHash || got.Clean != r.Clean() || got.Fingerprint != r.Fingerprint() {
		t.Errorf("last check %+v", got)
	}
	if got.Counts[drift.KindPending] != 1 || got.Counts[drift.KindInfo] != 1 || len(got.Counts) != 6 {
		t.Errorf("counts %v", got.Counts)
	}
	info, err := os.Stat(env.LastCheckPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
}

func TestDriftReportsExternal(t *testing.T) {
	env := home(t)
	e := newEngine(env, adapter.ClaudeCode, adapter.GeminiCLI)
	mustSync(t, e, "")
	claudeMD := at(env, ".claude/CLAUDE.md")
	replaceIn(t, claudeMD, "Write in plain language.", "Written by a script.")
	e.Config.External = []string{claudeMD}

	r := mustCheck(t, e, "")

	external := ofKind(r.Findings, drift.KindExternal)
	if len(external) != 1 || external[0].Path != claudeMD || external[0].Detail != "managed externally: "+claudeMD || external[0].Tool != adapter.ClaudeCode {
		t.Errorf("external:\n%s", dump(r.Findings))
	}
	for _, f := range r.Findings {
		if f.Path == claudeMD && f.Kind != drift.KindExternal {
			t.Errorf("external path also reported as %s", f.Kind)
		}
	}
	if !r.Clean() {
		t.Errorf("external finding makes the report unclean:\n%s", dump(r.Findings))
	}
}
