package cli

import (
	"fmt"
	"io"
)

type printer struct {
	w   io.Writer
	err error
}

func (p *printer) printf(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, args...)
	}
}

func (p *printer) done() error {
	if p.err != nil {
		return fmt.Errorf("print: %w", p.err)
	}
	return nil
}
