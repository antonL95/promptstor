package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/config"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/policy"
	"github.com/antonL95/promptstor/internal/secrets"
)

var errNoRunner = errors.New("running commands is not available")

func (d Deps) engine() (engine.Engine, error) {
	cfg, _, err := config.Load(d.Env.ConfigFile())
	if err != nil {
		return engine.Engine{}, err
	}
	acks, err := policy.LoadAcks(d.Env.AcksPath())
	if err != nil {
		return engine.Engine{}, err
	}
	guards, err := d.editGuards(cfg)
	if err != nil {
		return engine.Engine{}, err
	}
	return engine.Engine{
		Env:      d.Env,
		Config:   cfg,
		Adapters: adapter.All(),
		Resolver: secrets.OSResolver(d.Env, d.run),
		Acks:     acks,
		Now:      d.Now,
		Builtins: append(slices.Clone(d.Builtins), guards...),
		Notices:  editGuardNotices(cfg),
	}, nil
}

func (d Deps) run(name string, args ...string) ([]byte, error) {
	if d.Run == nil {
		return nil, errNoRunner
	}
	return d.Run(name, args...)
}

func absolute(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	return abs, nil
}
