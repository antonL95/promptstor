package mcpserver

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	projectLayerDir = ".promptstor"
	branchTime      = "20060102150405"
	commitName      = "PromptStor"
	commitEmail     = "promptstor@localhost"
	verbUpdate      = "update"
	verbRemove      = "remove"
)

type projectChange struct {
	id    library.ID
	name  string
	verb  string
	apply func(layerDir string, id library.ID) (string, error)
}

func (c projectChange) message() string {
	return "promptstor: " + c.verb + " " + c.name
}

func (s *server) upsertProject(in upsertInput) (any, error) {
	repo, err := s.repoRoot(in.ProjectDir)
	if err != nil {
		return nil, err
	}
	id := library.ID(in.Item.ID)
	if id == "" {
		id = library.NewID()
	}
	return s.changeProject(repo, projectChange{id: id, name: in.Item.Name, verb: verbUpdate, apply: func(dir string, id library.ID) (string, error) {
		items, err := library.LoadLayer(dir, library.LayerProject, library.LoadOptions{AssignTemporaryIDs: true})
		if err != nil {
			return "", err
		}
		current := find(items, id)
		if in.Item.ID != "" && current == nil {
			return "", fmt.Errorf("no item with id %s in the project layer; leave the id out to create a new item", id)
		}
		return store(dir, build(in.Item, id, current, library.LayerProject), current)
	}})
}

func (s *server) removeProject(in removeInput) (any, error) {
	repo, err := s.repoRoot(in.ProjectDir)
	if err != nil {
		return nil, err
	}
	id := library.ID(in.ID)
	items, err := library.LoadLayer(filepath.Join(repo, projectLayerDir), library.LayerProject, library.LoadOptions{AssignTemporaryIDs: true})
	if err != nil {
		return nil, err
	}
	current := find(items, id)
	if current == nil {
		return nil, fmt.Errorf("no item with id %s in the project layer", id)
	}
	return s.changeProject(repo, projectChange{id: id, name: current.Name, verb: verbRemove, apply: func(dir string, id library.ID) (string, error) {
		items, err := library.LoadLayer(dir, library.LayerProject, library.LoadOptions{AssignTemporaryIDs: true})
		if err != nil {
			return "", err
		}
		committed := find(items, id)
		if committed == nil {
			return "", fmt.Errorf("no item with id %s in the committed project layer", id)
		}
		return committed.Source, library.DeleteItem(dir, *committed)
	}})
}

func (s *server) repoRoot(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("the project layer needs project_dir")
	}
	abs, err := projectRoot(dir)
	if err != nil {
		return "", err
	}
	out, err := s.git(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository: %w", abs, err)
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}

func (s *server) changeProject(repo string, c projectChange) (any, error) {
	branch := fmt.Sprintf("promptstor/%s-%s", cmp.Or(library.Slug(c.name), "item"), s.now().UTC().Format(branchTime))
	worktrees := s.e.Env.WorktreeDir()
	if err := os.MkdirAll(worktrees, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", worktrees, err)
	}
	worktree := filepath.Join(worktrees, string(c.id))
	if _, err := os.Lstat(worktree); err == nil {
		return nil, fmt.Errorf("another change of %s is in progress at %s", c.id, worktree)
	}
	if _, err := s.git(repo, "worktree", "add", "-q", worktree, "-b", branch, "HEAD"); err != nil {
		return nil, err
	}
	view, err := s.commitChange(worktree, branch, c)
	if err != nil {
		s.discard(repo, worktree, branch)
		return nil, err
	}
	if _, err := s.git(repo, "worktree", "remove", worktree); err != nil {
		return nil, fmt.Errorf("committed branch %s, but the worktree stays at %s: %w", branch, worktree, err)
	}
	s.openPullRequest(repo, &view, c.message())
	return view, nil
}

func (s *server) commitChange(worktree, branch string, c projectChange) (changeView, error) {
	layerDir := filepath.Join(worktree, projectLayerDir)
	assigned, err := library.WriteMissingIDs(worktree, layerDir, library.LayerProject)
	if err != nil {
		return changeView{}, err
	}
	if stored, found := assigned.Renamed[c.id]; found {
		c.id = stored
	}
	path, err := c.apply(layerDir, c.id)
	if err != nil {
		return changeView{}, err
	}
	p, err := s.regenerate(worktree)
	if err != nil {
		return changeView{}, err
	}
	if _, err := s.git(worktree, "add", "-A"); err != nil {
		return changeView{}, err
	}
	staged, err := s.git(worktree, "diff", "--cached", "--name-only")
	if err != nil {
		return changeView{}, err
	}
	changed := strings.Fields(string(staged))
	if len(changed) == 0 {
		return changeView{}, fmt.Errorf("%s %s leaves the project unchanged", c.verb, c.name)
	}
	if err := s.commit(worktree, c.message()); err != nil {
		return changeView{}, err
	}
	view := changeView{Layer: string(library.LayerProject), ID: c.id, Name: c.name, Path: relative(worktree, path), Changed: changed, Branch: branch}
	view.report(p, worktree)
	return view, nil
}

func (s *server) regenerate(worktree string) (engine.Plan, error) {
	if err := os.MkdirAll(s.e.Env.StateDir(), 0o700); err != nil {
		return engine.Plan{}, fmt.Errorf("create %s: %w", s.e.Env.StateDir(), err)
	}
	scratch, err := os.MkdirTemp(s.e.Env.StateDir(), "project-")
	if err != nil {
		return engine.Plan{}, fmt.Errorf("create a scratch state: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	e := s.e
	e.Env.Home = filepath.Join(scratch, "home")
	e.Env.ConfigHome = filepath.Join(scratch, "config")
	e.Env.StateHome = filepath.Join(scratch, "state")
	if err := os.MkdirAll(e.Env.Home, 0o700); err != nil {
		return engine.Plan{}, fmt.Errorf("create %s: %w", e.Env.Home, err)
	}
	e.Store = backup.Store{}
	e.Resolver = nil
	e.Builtins = nil
	e.Notices = nil
	e.Config.ResolveSecrets = nil
	e.Config.Adopted = nil
	e.Config.ReplaceSymlinks = nil
	_, p, err := e.Sync(worktree, plan.Options{})
	if err != nil {
		return engine.Plan{}, fmt.Errorf("regenerate the project files: %w", err)
	}
	return p, nil
}

func (s *server) commit(worktree, message string) error {
	args := []string{"commit", "-q", "-m", message}
	if !s.hasAuthor(worktree) {
		args = append([]string{"-c", "user.name=" + commitName, "-c", "user.email=" + commitEmail}, args...)
	}
	_, err := s.git(worktree, args...)
	return err
}

func (s *server) hasAuthor(dir string) bool {
	for _, key := range []string{"user.name", "user.email"} {
		out, err := s.run(dir, "git", "config", "--get", key)
		if err != nil || strings.TrimSpace(string(out)) == "" {
			return false
		}
	}
	return true
}

func (s *server) discard(repo, worktree, branch string) {
	_, _ = s.git(repo, "worktree", "remove", "--force", worktree)
	_, _ = s.git(repo, "branch", "-D", branch)
}

func (s *server) openPullRequest(repo string, v *changeView, title string) {
	if !s.e.Config.OpenProjectPRs || s.e.Env.LookPath == nil {
		return
	}
	if _, err := s.e.Env.LookPath("gh"); err != nil {
		return
	}
	if _, err := s.git(repo, "push", "origin", v.Branch); err != nil {
		v.Warnings = append(v.Warnings, "the branch was not pushed: "+err.Error())
		return
	}
	body := "Changes the PromptStor project layer: " + title + ".\n"
	out, err := s.run(repo, "gh", "pr", "create", "--draft", "--head", v.Branch, "--title", title, "--body", body)
	if err != nil {
		v.Warnings = append(v.Warnings, "no pull request was opened: "+commandError(err).Error())
		return
	}
	v.PullRequest = strings.TrimSpace(string(out))
}

func (s *server) git(dir string, args ...string) ([]byte, error) {
	out, err := s.run(dir, "git", args...)
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), commandError(err))
	}
	return out, nil
}

func commandError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if stderr := bytes.TrimSpace(exit.Stderr); len(stderr) > 0 {
			return fmt.Errorf("%w: %s", err, stderr)
		}
	}
	return err
}
