package doctor

import (
	"fmt"
	"slices"

	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/plan"
	"github.com/antonL95/promptstor/internal/policy"
	"github.com/antonL95/promptstor/internal/preview"
)

const (
	questionHandEdit = "%s was changed outside PromptStor (%s). What should happen?"
	questionGrant    = "Promote runtime grant %s (%s, %s) into the library?"
	questionAck      = "Acknowledge %s for %s (%s)?"
	questionSync     = "Sync now?"
	handLeave        = "leave it"
	handPull         = "pull into the library"
	handOverwrite    = "overwrite with the library"
)

var handEditOptions = []string{handLeave, handPull, handOverwrite}

type staged struct {
	items      []stagedItem
	grants     []drift.Finding
	acks       policy.Acks
	ackChanged bool
	unresolved int
}

type stagedItem struct {
	dir string
	it  library.Item
}

func (s staged) empty() bool {
	return len(s.items) == 0 && len(s.grants) == 0 && !s.ackChanged
}

func (d *doctor) review() (int, error) {
	r, err := drift.Check(d.e, d.opt.Project)
	if err != nil {
		return 0, err
	}
	if err := d.render(r); err != nil {
		return 0, err
	}
	d.reportNotes(r)
	s := staged{acks: policy.Acks{List: slices.Clone(d.e.Acks.List)}}
	for _, f := range r.Findings {
		if err := d.resolve(f, &s); err != nil {
			return 0, err
		}
	}
	if s.unresolved > 0 {
		d.w.printf("\n%d hand edits are left as they are; nothing is synced until they are resolved.\n", s.unresolved)
		return 1, nil
	}
	if r.Clean() && s.empty() {
		d.w.printf("\nNothing to do.\n")
		return 0, nil
	}
	if d.opt.DryRun {
		d.w.printf("\ndry run: nothing was written\n")
		return 0, nil
	}
	ok, err := d.p.Confirm(questionSync, d.opt.Yes)
	if err != nil || !ok {
		return 0, err
	}
	if err := d.commit(s); err != nil {
		return 0, err
	}
	e := d.e
	e.Acks = s.acks
	p, err := e.Plan(d.opt.Project, plan.Options{})
	if err != nil {
		return 0, err
	}
	if d.w.err == nil {
		if _, err := preview.Plan(d.w.w, p, true); err != nil {
			return 0, err
		}
	}
	synced, err := d.sync(e)
	if err != nil {
		return 0, err
	}
	d.printNotes(synced)
	if changedCodexHooks(synced) {
		d.w.printf("Note: %s.\n", noteCodexHooks)
	}
	return 0, nil
}

func (d *doctor) reportNotes(r drift.Report) {
	plugins := false
	for _, f := range r.Findings {
		switch {
		case f.Kind == drift.KindSymlinked:
			d.w.printf(noteSymlinkFormat, f.Path)
		case f.Kind == drift.KindForeign && pluginForeign(f.Tool, f.Name):
			plugins = true
		}
	}
	if plugins {
		d.w.printf("Note: %s.\n", noteAntigravity)
	}
}

func (d *doctor) resolve(f drift.Finding, s *staged) error {
	switch f.Kind {
	case drift.KindHandEdit:
		at, err := d.p.Select(fmt.Sprintf(questionHandEdit, f.Path, f.Detail), handEditOptions, 0)
		if err != nil {
			return err
		}
		switch handEditOptions[at] {
		case handLeave:
			s.unresolved++
		case handPull:
			items, err := d.pullUnit(f)
			if err != nil {
				d.w.printf("cannot pull %s into the library: %v\n", f.Path, err)
				s.unresolved++
				return nil
			}
			s.items = append(s.items, items...)
		}
	case drift.KindRuntimeGrant:
		ok, err := d.p.Confirm(fmt.Sprintf(questionGrant, f.Name, f.Tool, f.Path), false)
		if err != nil {
			return err
		}
		if ok {
			s.grants = append(s.grants, f)
		}
	case drift.KindAckRequired:
		ok, err := d.p.Confirm(fmt.Sprintf(questionAck, f.Name, f.Tool, f.Detail), false)
		if err != nil {
			return err
		}
		if ok {
			return d.acknowledge(f, s)
		}
	}
	return nil
}

func (d *doctor) acknowledge(f drift.Finding, s *staged) error {
	merged, _, err := d.e.Library(d.opt.Project)
	if err != nil {
		return err
	}
	at := slices.IndexFunc(merged.Items, func(it library.Item) bool { return it.ID == f.Item })
	if at < 0 {
		return fmt.Errorf("acknowledge %s: item %s is not in the library", f.Name, f.Item)
	}
	s.acks.Add(policy.Ack{ItemID: f.Item, Tool: f.Tool, Hash: library.ContentHash(merged.Items[at]), At: d.now().UTC()})
	s.ackChanged = true
	return nil
}

func (d *doctor) writeMissingIDs(acks []policy.Ack) error {
	before, _, err := d.e.Library(d.opt.Project)
	if err != nil {
		return err
	}
	renamed, _, err := d.e.WriteMissingIDs(d.opt.Project)
	if err != nil || len(renamed) == 0 {
		return err
	}
	after, _, err := d.e.Library(d.opt.Project)
	if err != nil {
		return err
	}
	hashes := func(m library.Merged) map[library.ID]string {
		byID := map[library.ID]string{}
		for _, it := range m.Items {
			byID[it.ID] = library.ContentHash(it)
		}
		return byID
	}
	old, current := hashes(before), hashes(after)
	for i, ack := range acks {
		if id, found := renamed[ack.ItemID]; found && ack.Hash == old[ack.ItemID] {
			acks[i].ItemID, acks[i].Hash = id, current[id]
		}
	}
	return nil
}

func (d *doctor) commit(s staged) error {
	if err := d.writeMissingIDs(s.acks.List); err != nil {
		return err
	}
	items := slices.Clone(s.items)
	promoted, err := d.promote(s.grants)
	if err != nil {
		return err
	}
	items = append(items, promoted...)
	for _, si := range items {
		if _, err := library.WriteItem(si.dir, si.it); err != nil {
			return err
		}
	}
	if s.ackChanged {
		return s.acks.Save(d.e.Env.AcksPath())
	}
	return nil
}
