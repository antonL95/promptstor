package doctor

import (
	"fmt"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
)

const (
	questionPrimary   = "Which tool is your primary? Its version wins when tools disagree."
	questionTools     = "Which tools should PromptStor keep in sync?"
	projectFilesOnly  = " (project files only)"
	noToolsDetected   = "No supported agent was found on this machine. Nothing to do.\n"
	errUnknownPrimary = "primary tool %q is not installed"
	errUnknownTool    = "tool %q is not installed"
)

type detected struct {
	a         adapter.Adapter
	installed bool
	paths     []string
}

func (d *doctor) detect() []detected {
	var found []detected
	for _, a := range d.e.Adapters {
		if a.ID() == adapter.AgentsMD {
			continue
		}
		det := a.Detect(d.e.Env)
		found = append(found, detected{a: a, installed: det.Installed || len(det.Paths) > 0, paths: det.Paths})
	}
	return found
}

func (d *doctor) printDetected(found []detected) {
	width := len("tool")
	for _, t := range found {
		width = max(width, len(t.a.ID()))
	}
	d.w.printf("%-*s  %-9s  %s\n", width, "tool", "installed", "paths")
	for _, t := range found {
		paths := strings.Join(t.paths, ", ")
		if paths == "" {
			paths = "-"
		}
		d.w.printf("%-*s  %-9s  %s\n", width, t.a.ID(), yesNo(t.installed), paths)
	}
}

func installedTools(found []detected) []string {
	var tools []string
	for _, t := range found {
		if t.installed {
			tools = append(tools, t.a.ID())
		}
	}
	return tools
}

func (d *doctor) choosePrimary(installed []string, counts map[string]int) (string, error) {
	if d.opt.Primary != "" {
		if !slices.Contains(installed, d.opt.Primary) {
			return "", fmt.Errorf(errUnknownPrimary, d.opt.Primary)
		}
		return d.opt.Primary, nil
	}
	def := 0
	for i, tool := range installed {
		if counts[tool] > counts[installed[def]] {
			def = i
		}
	}
	labels := make([]string, len(installed))
	for i, tool := range installed {
		labels[i] = fmt.Sprintf("%s (%d items)", tool, counts[tool])
	}
	at, err := d.p.Select(questionPrimary, labels, def)
	if err != nil {
		return "", err
	}
	return installed[at], nil
}

func (d *doctor) chooseTools(found []detected, primary string) ([]string, error) {
	installed := installedTools(found)
	var tools []string
	if len(d.opt.Tools) > 0 {
		for _, tool := range d.opt.Tools {
			if !slices.Contains(installed, tool) && tool != adapter.Cursor {
				return nil, fmt.Errorf(errUnknownTool, tool)
			}
			tools = append(tools, tool)
		}
	} else {
		options := slices.Clone(installed)
		labels := slices.Clone(installed)
		if !slices.Contains(installed, adapter.Cursor) && slices.ContainsFunc(found, func(t detected) bool { return t.a.ID() == adapter.Cursor }) {
			options = append(options, adapter.Cursor)
			labels = append(labels, adapter.Cursor+projectFilesOnly)
		}
		var defs []int
		for i := range installed {
			defs = append(defs, i)
		}
		picked, err := d.p.MultiSelect(questionTools, labels, defs)
		if err != nil {
			return nil, err
		}
		for _, at := range picked {
			tools = append(tools, options[at])
		}
	}
	if !slices.Contains(tools, primary) {
		tools = append(tools, primary)
	}
	slices.Sort(tools)
	return slices.Compact(tools), nil
}
