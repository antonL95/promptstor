package claudecode

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/library"
)

const (
	skillFile         = "SKILL.md"
	syncedDir         = "synced"
	syncedOwner       = "claude.ai"
	syncedReason      = "synced skill; Claude Code overwrites it"
	pluginsOwner      = "claude-code plugins"
	pluginsReason     = "installed plugins; Claude Code manages them"
	boostSkillReason  = "listed in boost.json; Boost regenerates it"
	credentialSkipped = "matches the credential exclusion list; not imported"
	commandSeparator  = ":"
)

func (im *importer) commands() error {
	return im.markdownFiles(im.l.in(commandsDir), func(path, relative string) error {
		name := strings.ReplaceAll(strings.TrimSuffix(relative, filepath.Ext(relative)), "/", commandSeparator)
		it, err := readNative(path, library.KindCommand, name)
		if err != nil {
			im.note(path, unreadableFront, err)
			return nil
		}
		it.Name = name
		im.out.Items = append(im.out.Items, it)
		return nil
	})
}

func (im *importer) subagents() error {
	return im.markdownFiles(im.l.in(agentsDir), func(path, relative string) error {
		it, err := readNative(path, library.KindSubagent, strings.TrimSuffix(filepath.Base(relative), filepath.Ext(relative)))
		if err != nil {
			im.note(path, unreadableFront, err)
			return nil
		}
		im.out.Items = append(im.out.Items, it)
		return nil
	})
}

func (im *importer) skills() error {
	dir := im.l.in(skillsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", dir, err)
	}
	boosted := im.boostSkills()
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		switch {
		case strings.HasPrefix(entry.Name(), "."):
		case strings.EqualFold(entry.Name(), syncedDir) && entry.IsDir():
			if err := im.syncedSkills(path); err != nil {
				return err
			}
		case entry.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(path)
			im.note(path, noteSymlinked, target)
		case !entry.IsDir():
		case slices.Contains(boosted, entry.Name()):
			im.foreign(path, boostOwner, boostSkillReason)
		default:
			if err := im.skill(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (im *importer) syncedSkills(dir string) error {
	return walkFiles(dir, func(path, relative string, entry fs.DirEntry) error {
		if entry.Name() == skillFile && strings.Count(relative, "/") == 2 {
			im.foreign(filepath.Dir(path), syncedOwner, syncedReason)
		}
		return nil
	})
}

func (im *importer) skill(dir string) error {
	main := filepath.Join(dir, skillFile)
	if entry, err := os.Lstat(main); err != nil || !entry.Mode().IsRegular() {
		return nil
	}
	it, err := readNative(main, library.KindSkill, filepath.Base(dir))
	if err != nil {
		im.note(main, unreadableFront, err)
		return nil
	}
	it.Source = dir
	err = walkFiles(dir, func(path, relative string, entry fs.DirEntry) error {
		switch {
		case relative == skillFile:
			return nil
		case entry.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(path)
			im.note(path, noteSymlinked, target)
			return nil
		case !entry.Type().IsRegular():
			return nil
		case backup.Excluded(im.t.Env, path):
			im.note(path, credentialSkipped)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if it.Files == nil {
			it.Files = map[string]library.File{}
		}
		it.Files[relative] = library.File{Data: data, Mode: info.Mode().Perm()}
		return nil
	})
	if err != nil {
		return err
	}
	im.out.Items = append(im.out.Items, it)
	return nil
}

func (im *importer) boostSkills() []string {
	var config struct {
		Skills []string `json:"skills"`
	}
	if im.l.scope != adapter.ScopeProject {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(im.l.root, boostConfigName))
	if err != nil || json.Unmarshal(data, &config) != nil {
		return nil
	}
	return config.Skills
}

func (im *importer) foreignState() error {
	if im.l.scope != adapter.ScopeUser {
		return nil
	}
	if plugins := im.l.in(pluginsDir); exists(plugins) {
		im.foreign(plugins, pluginsOwner, pluginsReason)
	}
	if im.t.Env.Getenv != nil && im.t.Env.Getenv(configDirVariable) != "" {
		im.note(im.l.config, noteConfigDir)
	}
	return nil
}
