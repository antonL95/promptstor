package ci

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	codeInSync  = 0
	codeDiffers = 1
	codeError   = 2
)

func Check(root string, out io.Writer) (int, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return codeError, fmt.Errorf("resolve %s: %w", root, err)
	}
	p, err := load(root)
	if err != nil {
		return codeError, err
	}
	generated, err := p.export()
	if err != nil {
		return codeError, err
	}
	changes, err := onDisk(generated.units, p.tools)
	if err != nil {
		return codeError, err
	}
	r := &report{w: out, root: root}
	for _, change := range changes {
		if err := r.compare(change); err != nil {
			return codeError, err
		}
	}
	r.literals(generated.literals)
	r.unsupported(generated.unsupported)
	return r.finish()
}

func onDisk(units []plan.Unit, tools []string) ([]plan.FileChange, error) {
	var compared []string
	for _, u := range units {
		compared = append(compared, u.Path)
	}
	changes, err := plan.Diff(units, manifest.Manifest{}, tools, plan.Options{Adopt: compared})
	if err != nil {
		return nil, fmt.Errorf("compare the project files: %w", err)
	}
	return changes, nil
}
