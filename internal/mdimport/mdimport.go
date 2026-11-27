package mdimport

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	ReasonCycle      = "cycle"
	ReasonTilde      = "tilde"
	ReasonOutside    = "outside git root"
	ReasonDepth      = "depth"
	ReasonMissing    = "missing"
	ReasonSyntax     = "syntax"
	ReasonCredential = "credential file"
)

type Syntax struct {
	Name                                        string
	Line                                        *regexp.Regexp
	AllowTilde, AllowAbsolute, ConfineToGitRoot bool
	MaxDepth                                    int
}

var Claude = Syntax{
	Name:          "claude-code",
	Line:          regexp.MustCompile(`(?:^|\s)@(?:(?P<path>(?:\\ |[^\s"'\\])+)|\S+)`),
	AllowTilde:    true,
	AllowAbsolute: true,
	MaxDepth:      4,
}

var Gemini = Syntax{
	Name:             "gemini-cli",
	Line:             regexp.MustCompile(`(?:^|\s)@(?:(?P<path>[~./A-Za-z]\S*)|\S+)`),
	AllowAbsolute:    true,
	ConfineToGitRoot: true,
	MaxDepth:         5,
}

var Antigravity = Syntax{
	Name:          "antigravity",
	Line:          regexp.MustCompile(`(?:^|\s)@\[[^\]]*\]\((?P<path>[^)]*)\)`),
	AllowTilde:    true,
	AllowAbsolute: true,
	MaxDepth:      5,
}

type ResolvedFile struct {
	Path  string
	Body  []byte
	Depth int
}

type Resolved struct {
	Files      []ResolvedFile
	Unresolved []adapter.Unresolved
}

type resolver struct {
	env     paths.Env
	syn     Syntax
	visited map[string]bool
	stack   []string
	out     Resolved
}

func Resolve(env paths.Env, path string, syn Syntax) (Resolved, error) {
	r := &resolver{env: env, syn: syn, visited: map[string]bool{}}
	if err := r.visit(filepath.Clean(path), 0); err != nil {
		return Resolved{}, err
	}
	return r.out, nil
}

func (r *resolver) visit(path string, depth int) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	r.visited[path] = true
	r.out.Files = append(r.out.Files, ResolvedFile{Path: path, Body: body, Depth: depth})
	r.stack = append(r.stack, path)
	defer func() { r.stack = r.stack[:len(r.stack)-1] }()
	for _, found := range Find(body, r.syn) {
		target, reason := r.target(path, found)
		if reason == "" && depth+1 > r.syn.MaxDepth {
			reason = ReasonDepth
		}
		if reason == "" && r.visited[target] {
			continue
		}
		if reason == "" {
			reason = missing(target)
		}
		if reason == "" && r.credential(target) {
			reason = ReasonCredential
		}
		if reason != "" {
			r.out.Unresolved = append(r.out.Unresolved, adapter.Unresolved{
				File: path, Line: strconv.Itoa(found.Line), Target: found.Token, Reason: reason,
			})
			continue
		}
		if err := r.visit(target, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (r *resolver) target(from string, found Import) (string, string) {
	if found.Path == "" || strings.Contains(found.Path, "://") {
		return "", ReasonSyntax
	}
	dir := filepath.Dir(from)
	var target string
	switch {
	case found.Path == "~" || strings.HasPrefix(found.Path, "~/"):
		if !r.syn.AllowTilde {
			return "", ReasonTilde
		}
		target = r.env.Expand(found.Path)
	case strings.HasPrefix(found.Path, "~"):
		if !r.syn.AllowTilde {
			return "", ReasonTilde
		}
		return "", ReasonSyntax
	case filepath.IsAbs(found.Path):
		if !r.syn.AllowAbsolute {
			return "", ReasonSyntax
		}
		target = found.Path
	default:
		target = filepath.Join(dir, filepath.FromSlash(found.Path))
	}
	target = filepath.Clean(target)
	if r.syn.ConfineToGitRoot && !within(gitRoot(dir), target) {
		return "", ReasonOutside
	}
	if slices.Contains(r.stack, target) {
		return "", ReasonCycle
	}
	return target, ""
}

func (r *resolver) credential(path string) bool {
	if backup.Excluded(r.env, path) {
		return true
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err != nil || backup.Excluded(r.env, resolved)
}

func missing(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ReasonMissing
	}
	return ""
}

func gitRoot(dir string) string {
	for current := dir; ; {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return dir
		}
		current = parent
	}
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && filepath.IsLocal(relative)
}
