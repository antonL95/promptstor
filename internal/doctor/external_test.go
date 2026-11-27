package doctor_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/doctor"
	"github.com/antonL95/promptstor/internal/drift"
)

const otherWriter = "Which of them does another tool or script write?"

func TestDoctorMarksExternalAndSkipsIt(t *testing.T) {
	env := fixtureHome(t)
	codex := read(t, codexMD(env))

	out, s := firstRun(t, env, answer{otherWriter, codexMD(env)})

	if !s.wasAsked("PromptStor would change these files. " + otherWriter + " PromptStor will leave those alone.") {
		t.Fatalf("external question not asked: %v", s.asked)
	}
	pull := slices.IndexFunc(s.asked, func(q string) bool { return strings.Contains(q, `Pull rule "Commits"`) })
	ext := slices.IndexFunc(s.asked, func(q string) bool { return strings.Contains(q, otherWriter) })
	moved := slices.IndexFunc(s.asked, func(q string) bool { return strings.Contains(q, moveRules) })
	if pull < 0 || pull > ext || (moved >= 0 && moved < ext) {
		t.Errorf("question order: %v", s.asked)
	}
	if s.wasAsked(moveRules + " " + codexMD(env)) {
		t.Error("offered to move the rules of an external file")
	}
	if got := read(t, codexMD(env)); got != codex {
		t.Errorf("external codex AGENTS.md was written:\n%s", got)
	}
	if !strings.Contains(out, "managed externally: "+codexMD(env)) {
		t.Errorf("external file not reported:\n%s", out)
	}
	if !strings.Contains(read(t, claudeMD(env)), "imperative mood") {
		t.Errorf("rules from the external file were not pulled for claude-code:\n%s", read(t, claudeMD(env)))
	}
	cfg, _, err := config.Load(env.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.External, []string{codexMD(env)}) {
		t.Errorf("config external %v", cfg.External)
	}

	later, _ := run(t, env, &script{}, doctor.Options{DryRun: true})
	if !strings.Contains(later, string(drift.KindExternal)) || !strings.Contains(later, codexMD(env)) {
		t.Errorf("later run does not list the external file:\n%s", later)
	}
	if got := read(t, codexMD(env)); got != codex {
		t.Error("later run wrote the external file")
	}
}

type externalProbe struct {
	*script
	options []string
}

func (p *externalProbe) MultiSelect(q string, opts []string, defs []int) ([]int, error) {
	if strings.Contains(q, otherWriter) {
		p.options = opts
	}
	return p.script.MultiSelect(q, opts, defs)
}

func TestExternalQuestionListsOnlyWritableFiles(t *testing.T) {
	env := fixtureHome(t)
	skills := filepath.Join(env.Home, ".claude", "skills")
	linkedSkills := filepath.Join(env.Home, "dotfiles", "skills")
	if err := os.MkdirAll(filepath.Dir(linkedSkills), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(skills, linkedSkills); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "dotfiles", "skills"), skills); err != nil {
		t.Fatal(err)
	}
	commands := filepath.Join(env.Home, ".claude", "commands")
	for _, name := range []string{"one", "two", "three", "four", "five", "six"} {
		write(t, filepath.Join(commands, name+".md"), "Run the "+name+" check.\n")
	}
	p := &externalProbe{script: &script{}}

	run(t, env, p, doctor.Options{DryRun: true})

	if p.options == nil {
		t.Fatal("external question not asked")
	}
	for _, opt := range p.options {
		if strings.HasPrefix(opt, skills) || strings.HasPrefix(opt, linkedSkills) {
			t.Errorf("blocked path listed: %s", opt)
		}
		if strings.HasPrefix(opt, commands+string(filepath.Separator)) {
			t.Errorf("file not collapsed into its directory: %s", opt)
		}
	}
	if !slices.Contains(p.options, commands) {
		t.Errorf("commands directory not listed: %v", p.options)
	}
	if !slices.Contains(p.options, codexMD(env)) {
		t.Errorf("written file not listed: %v", p.options)
	}
}

func TestDoctorExternalDryRunSavesNothing(t *testing.T) {
	env := fixtureHome(t)

	run(t, env, &script{answers: []answer{{otherWriter, codexMD(env)}}}, doctor.Options{DryRun: true})

	if exists(env.ConfigFile()) {
		t.Error("dry run saved the external choice")
	}
}
