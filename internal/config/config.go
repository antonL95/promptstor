package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/antonL95/promptstor/internal/fsx"
)

const (
	currentVersion = 1
	fileMode       = fs.FileMode(0o600)
	projectDir     = ".promptstor"
	fileName       = "config.yaml"
)

type Config struct {
	Version          int      `yaml:"version"`
	Primary          string   `yaml:"primary,omitempty"`
	Tools            []string `yaml:"tools,omitempty"`
	FirstRunSnapshot string   `yaml:"first_run_snapshot,omitempty"`
	Adopted          []string `yaml:"adopted,omitempty"`
	ReplaceSymlinks  []string `yaml:"replace_symlinks,omitempty"`
	EditGuards       bool     `yaml:"edit_guards"`
	RegisterMCP      bool     `yaml:"register_mcp"`
	OpenProjectPRs   bool     `yaml:"open_project_prs"`
	ResolveSecrets   []string `yaml:"resolve_secrets,omitempty"`
}

type Project struct {
	Tools []string `yaml:"tools,omitempty"`
}

func Load(path string) (Config, bool, error) {
	c := Config{Version: currentVersion}
	found, err := decode(path, &c)
	if err != nil || !found {
		return c, found, err
	}
	if c.Version != currentVersion {
		return Config{}, true, fmt.Errorf("load %s: unsupported version %d", path, c.Version)
	}
	return c, true, nil
}

func (c Config) Save(path string) error {
	c.Version = currentVersion
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(c); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	if err := fsx.WriteAtomic(dir, path, buffer.Bytes(), fileMode); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	return nil
}

func LoadProject(root string) (Project, bool, error) {
	var p Project
	found, err := decode(filepath.Join(root, projectDir, fileName), &p)
	return p, found, err
}

func decode(path string, target any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("load %s: %w", path, err)
	}
	return true, nil
}
