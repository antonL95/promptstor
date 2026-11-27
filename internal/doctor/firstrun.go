package doctor

import (
	"fmt"
	"slices"

	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	questionGuards = "Install edit guards in every selected tool that supports them?"
	questionMCP    = "Register the promptstor MCP server in every selected tool?"
	noteGuards     = "\nEdit guards stop agents from editing the files PromptStor owns. Antigravity gets no edit guard in this release.\n"
)

type firstRunState struct {
	found    []detected
	scopes   []scopeImports
	primary  string
	selected []string
	cands    []*candidate
	moves    []*move
	adoption adoption
	snapshot string
	external []string
}

func (d *doctor) firstRun() (int, error) {
	d.w.printf("PromptStor doctor: first run\n\n")
	s := &firstRunState{found: d.detect()}
	d.printDetected(s.found)
	installed := installedTools(s.found)
	if len(installed) == 0 {
		d.w.printf(noToolsDetected)
		return 0, nil
	}
	steps := []func(*firstRunState) error{d.discover, d.compareAndPull, d.askExternal, d.adopt, d.snapshot, d.writeLibrary}
	for _, step := range steps {
		if err := step(s); err != nil {
			return 0, err
		}
	}
	cfg, err := d.configure(s)
	if err != nil {
		return 0, err
	}
	return d.planAndApply(s, cfg)
}

func (d *doctor) discover(s *firstRunState) error {
	installed := installedTools(s.found)
	scopes, err := d.importAll(installed)
	if err != nil {
		return err
	}
	s.scopes = scopes
	if s.primary, err = d.choosePrimary(installed, itemCounts(scopes)); err != nil {
		return err
	}
	s.selected, err = d.chooseTools(s.found, s.primary)
	return err
}

func (d *doctor) compareAndPull(s *firstRunState) error {
	d.printComparison(s.scopes)
	cands, err := d.pull(s.scopes, s.primary, s.selected)
	s.cands = cands
	return err
}

func (d *doctor) adopt(s *firstRunState) error {
	moves, err := d.collectMoves(s.scopes, s.cands, s.selected, s.external)
	if err != nil {
		return err
	}
	s.moves = moves
	if err := d.askMoves(s.moves); err != nil {
		return err
	}
	if err := d.prepareMoves(s.moves, false); err != nil {
		return err
	}
	a, err := d.collectFiles(s.cands, s.isExternal)
	s.adoption = a
	return err
}

func (d *doctor) snapshot(s *firstRunState) error {
	if d.opt.DryRun {
		d.w.printf("\ndry run: no snapshot is taken\n")
		return nil
	}
	entries := d.snapshotEntries(s.found)
	entries = mergeEntries(entries, d.changedFileEntries(s))
	id, err := d.takeEntries(entries)
	if err != nil {
		return fmt.Errorf("take the first-run snapshot, nothing was written: %w", err)
	}
	s.snapshot = id
	d.w.printf("\nfirst-run snapshot %s (undo everything with: promptstor restore %s)\n", id, id)
	return nil
}

func (d *doctor) writeLibrary(s *firstRunState) error {
	if err := d.storeLibrary(s.cands); err != nil {
		return err
	}
	return d.prepareMoves(s.moves, true)
}

func (d *doctor) configure(s *firstRunState) (config.Config, error) {
	cfg := config.Config{Version: 1, Primary: s.primary, Tools: s.selected, Adopted: s.adoption.adopt, External: s.external}
	d.w.printf(noteGuards)
	guards, err := d.p.Confirm(questionGuards, true)
	if err != nil {
		return cfg, err
	}
	register, err := d.p.Confirm(questionMCP, true)
	if err != nil {
		return cfg, err
	}
	cfg.EditGuards, cfg.RegisterMCP = guards, register
	return cfg, nil
}

func (d *doctor) planAndApply(s *firstRunState, cfg config.Config) (int, error) {
	e, err := d.engineFor(cfg)
	if err != nil {
		return 0, err
	}
	if d.opt.DryRun {
		e.Builtins = append(slices.Clone(e.Builtins), storedItems(s.cands)...)
	}
	p, err := e.Plan(d.opt.Project, plan.Options{})
	if err != nil {
		return 0, err
	}
	if err := d.printPreview(s, p); err != nil {
		return 0, err
	}
	if d.opt.DryRun {
		d.w.printf("\ndry run: nothing was written\n")
		return 0, nil
	}
	apply, err := d.confirmApply(s, p)
	if err != nil || !apply {
		if err == nil {
			d.w.printf("\nNothing changed in your tools. The library and the first-run snapshot were kept.\n")
		}
		return 0, err
	}
	if err := d.applyFiles(s); err != nil {
		return 0, err
	}
	synced, err := d.sync(e)
	if err != nil {
		return 0, err
	}
	cfg.FirstRunSnapshot = s.snapshot
	if err := cfg.Save(e.Env.ConfigFile()); err != nil {
		return 0, err
	}
	d.printNotes(synced)
	if changedCodexHooks(synced) {
		d.w.printf("Note: %s.\n", noteCodexHooks)
	}
	return 0, nil
}

func (d *doctor) sync(e engine.Engine) (engine.Plan, error) {
	result, p, err := e.Sync(d.opt.Project, plan.Options{})
	if result.SnapshotID != "" {
		d.w.printf("snapshot %s\n", result.SnapshotID)
	}
	for _, path := range result.Written {
		d.w.printf("wrote %s\n", path)
	}
	return p, err
}
