package doctor

import (
	"errors"

	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/drift"
)

var errNoConfig = errors.New("promptstor has no configuration yet; run promptstor doctor first")

func (d *doctor) check() (int, error) {
	_, found, err := config.Load(d.e.Env.ConfigFile())
	if err != nil {
		return 2, err
	}
	if !found {
		return 2, errNoConfig
	}
	r, err := drift.Check(d.e, d.opt.Project)
	if err != nil {
		return 2, err
	}
	if err := d.render(r); err != nil {
		return 2, err
	}
	if !d.opt.DryRun {
		if err := d.record(r); err != nil {
			return 2, err
		}
	}
	if r.Clean() {
		return 0, nil
	}
	return 1, nil
}

func (d *doctor) record(r drift.Report) error {
	var notifyErr error
	if d.opt.Notify && d.opt.Notifier != nil {
		notifyErr = d.opt.Notifier.Notify(d.e.Env, r)
	}
	return errors.Join(notifyErr, drift.WriteLastCheck(d.e.Env, r, d.now()))
}

func (d *doctor) render(r drift.Report) error {
	if d.opt.JSON {
		if d.w.err != nil {
			return d.w.done()
		}
		return drift.RenderJSON(d.w.w, r)
	}
	if d.w.err == nil {
		drift.RenderText(d.w.w, r)
	}
	return d.w.done()
}
