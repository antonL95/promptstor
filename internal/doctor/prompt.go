package doctor

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

var (
	ErrNoAnswer   = errors.New("no answer")
	errNoTerminal = errors.New("doctor needs a terminal or --yes")
)

type Prompter interface {
	Select(q string, opts []string, def int) (int, error)
	MultiSelect(q string, opts []string, defs []int) ([]int, error)
	Confirm(q string, def bool) (bool, error)
}

type linePrompter struct {
	in    *bufio.Reader
	out   io.Writer
	isTTY bool
	yes   bool
}

func LinePrompter(in io.Reader, out io.Writer, isTTY bool, yes bool) Prompter {
	return &linePrompter{in: bufio.NewReader(in), out: out, isTTY: isTTY, yes: yes}
}

func (p *linePrompter) Select(q string, opts []string, def int) (int, error) {
	if err := p.ready(); err != nil {
		return 0, err
	}
	p.options(q, opts)
	if p.yes {
		p.printf("%s: %s (--yes)\n", q, opts[def])
		return def, nil
	}
	for {
		line, err := p.ask(fmt.Sprintf("choose [%d]: ", def+1))
		if err != nil {
			return 0, err
		}
		if line == "" {
			return def, nil
		}
		if at, ok := pick(line, opts); ok {
			return at, nil
		}
		p.printf("answer with a number from 1 to %d or a label\n", len(opts))
	}
}

func (p *linePrompter) MultiSelect(q string, opts []string, defs []int) ([]int, error) {
	if err := p.ready(); err != nil {
		return nil, err
	}
	p.options(q, opts)
	if p.yes {
		var labels []string
		for _, at := range defs {
			labels = append(labels, opts[at])
		}
		p.printf("%s: %s (--yes)\n", q, strings.Join(labels, ", "))
		return defs, nil
	}
	var shown []string
	for _, at := range defs {
		shown = append(shown, strconv.Itoa(at+1))
	}
	for {
		line, err := p.ask(fmt.Sprintf("choose, separated by commas [%s]: ", strings.Join(shown, ",")))
		if err != nil {
			return nil, err
		}
		if line == "" {
			return defs, nil
		}
		if picked, ok := pickAll(line, opts); ok {
			return picked, nil
		}
		p.printf("answer with numbers from 1 to %d or labels, separated by commas\n", len(opts))
	}
}

func (p *linePrompter) Confirm(q string, def bool) (bool, error) {
	if err := p.ready(); err != nil {
		return false, err
	}
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	if p.yes {
		p.printf("%s [%s]: %s (--yes)\n", q, hint, yesNo(def))
		return def, nil
	}
	for {
		line, err := p.ask(fmt.Sprintf("%s [%s]: ", q, hint))
		if err != nil {
			return false, err
		}
		switch strings.ToLower(line) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		p.printf("answer yes or no\n")
	}
}

func (p *linePrompter) ready() error {
	if !p.isTTY && !p.yes {
		return errNoTerminal
	}
	return nil
}

func (p *linePrompter) options(q string, opts []string) {
	p.printf("%s\n", q)
	for i, opt := range opts {
		p.printf("  %d) %s\n", i+1, opt)
	}
}

func (p *linePrompter) ask(prompt string) (string, error) {
	p.printf("%s", prompt)
	line, err := p.in.ReadString('\n')
	if errors.Is(err, io.EOF) && line == "" {
		return "", ErrNoAnswer
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read answer: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func (p *linePrompter) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(p.out, format, args...)
}

func pick(answer string, opts []string) (int, bool) {
	if n, err := strconv.Atoi(answer); err == nil {
		return n - 1, n >= 1 && n <= len(opts)
	}
	at := slices.IndexFunc(opts, func(opt string) bool { return strings.EqualFold(opt, answer) })
	return at, at >= 0
}

func pickAll(answer string, opts []string) ([]int, bool) {
	var picked []int
	for _, part := range strings.Split(answer, ",") {
		at, ok := pick(strings.TrimSpace(part), opts)
		if !ok {
			return nil, false
		}
		if !slices.Contains(picked, at) {
			picked = append(picked, at)
		}
	}
	return picked, true
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
