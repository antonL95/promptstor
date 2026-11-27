package doctor

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	projectLayerDir    = ".promptstor"
	noteCodexHooks     = "Codex skips changed hooks until you trust them again in /hooks"
	noteAntigravity    = "Antigravity imported plugins may duplicate PromptStor items and hold secrets in plain text"
	noteSymlinkFormat  = "%s is a symlink; PromptStor leaves it to whoever manages the link\n"
	codexHooksPrefix   = "/hooks/"
	antigravityPlugins = "antigravity plugins"
	antigravityImports = "antigravity importer"
)

type Notifier interface {
	Notify(env paths.Env, r drift.Report) error
}

type Options struct {
	DryRun, Check, Notify, Yes, JSON bool
	Primary                          string
	Tools                            []string
	Project                          string
	Notifier                         Notifier
	Reconfigure                      func(config.Config) (engine.Engine, error)
}

type doctor struct {
	e   engine.Engine
	p   Prompter
	w   *writer
	opt Options
}

func Run(e engine.Engine, p Prompter, out io.Writer, opt Options) (int, error) {
	d := &doctor{e: e, p: p, w: &writer{w: out}, opt: opt}
	if opt.Check {
		return d.check()
	}
	_, found, err := config.Load(e.Env.ConfigFile())
	if err != nil {
		return 2, err
	}
	var code int
	if found {
		code, err = d.review()
	} else {
		code, err = d.firstRun()
	}
	if err != nil {
		return 2, err
	}
	return code, d.w.done()
}

type writer struct {
	w   io.Writer
	err error
}

func (w *writer) printf(format string, args ...any) {
	if w.err == nil {
		_, w.err = fmt.Fprintf(w.w, format, args...)
	}
}

func (w *writer) done() error {
	if w.err != nil {
		return fmt.Errorf("print: %w", w.err)
	}
	return nil
}

func (d *doctor) engineFor(cfg config.Config) (engine.Engine, error) {
	if d.opt.Reconfigure != nil {
		return d.opt.Reconfigure(cfg)
	}
	e := d.e
	e.Config = cfg
	return e, nil
}

func (d *doctor) now() time.Time {
	if d.e.Now == nil {
		return time.Now()
	}
	return d.e.Now()
}

func (d *doctor) layerDir(layer library.Layer) (string, error) {
	switch layer {
	case library.LayerPersonal, "":
		return d.e.Env.LibraryDir(), nil
	case library.LayerDevice:
		return d.e.Env.DeviceDir(), nil
	case library.LayerProject:
		if d.opt.Project != "" {
			return filepath.Join(d.opt.Project, projectLayerDir), nil
		}
	}
	return "", fmt.Errorf("the %s layer is not edited by doctor", layer)
}

func (d *doctor) printNotes(p engine.Plan) {
	for _, c := range p.Changes {
		if strings.HasPrefix(c.Blocked, plan.ReasonSymlinked) {
			d.w.printf(noteSymlinkFormat, c.Path)
		}
	}
}

func changedCodexHooks(p engine.Plan) bool {
	return slices.ContainsFunc(p.Changes, func(c plan.FileChange) bool {
		if c.Blocked != "" || (c.Op == plan.OpNoop && len(c.Removed) == 0) {
			return false
		}
		for _, u := range c.Units {
			if slices.Contains(strings.Split(u.Tool, ","), adapter.Codex) && strings.HasPrefix(u.Key, codexHooksPrefix) {
				return true
			}
		}
		for _, entry := range c.Removed {
			if slices.Contains(entry.Tools(), adapter.Codex) && strings.HasPrefix(entry.Key, codexHooksPrefix) {
				return true
			}
		}
		return false
	})
}

func pluginForeign(tool, owner string) bool {
	return tool == adapter.Antigravity && (owner == antigravityPlugins || owner == antigravityImports)
}
