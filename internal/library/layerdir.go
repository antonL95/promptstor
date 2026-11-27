package library

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/fsx"
)

type LoadOptions struct{ AssignIDs, AssignTemporaryIDs bool }

func LoadLayer(dir string, layer Layer, opt LoadOptions) ([]Item, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("load layer %s: %w", dir, err)
	}
	var items []Item
	for _, l := range layouts {
		loaded, err := loadKind(dir, l)
		if err != nil {
			return nil, err
		}
		items = append(items, loaded...)
	}
	for index := range items {
		items[index].Layer = layer
	}
	if err := checkIDs(dir, items, opt); err != nil {
		return nil, err
	}
	return items, assignIDs(dir, items)
}

func listDir(path string) ([]fs.DirEntry, error) {
	entry, err := fsx.Inspect(path)
	if err != nil || !entry.Exists {
		return nil, err
	}
	if entry.IsLink {
		return nil, symlinkError(path)
	}
	all, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var visible []fs.DirEntry
	for _, child := range all {
		if strings.HasPrefix(child.Name(), ".") {
			continue
		}
		if child.Type()&fs.ModeSymlink != 0 {
			return nil, symlinkError(filepath.Join(path, child.Name()))
		}
		visible = append(visible, child)
	}
	return visible, nil
}

func loadKind(dir string, l layout) ([]Item, error) {
	entries, err := listDir(filepath.Join(dir, l.dir))
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, entry := range entries {
		path := filepath.Join(dir, l.dir, entry.Name())
		var slug string
		switch {
		case l.kind == KindSkill && entry.IsDir():
			slug = entry.Name()
		case l.kind != KindSkill && entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), l.extension):
			slug = strings.TrimSuffix(entry.Name(), l.extension)
		case l.folder && l.kind != KindSkill && entry.IsDir():
			if err := checkOwner(dir, l, entry.Name()); err != nil {
				return nil, err
			}
			continue
		case l.kind == KindSkill:
			return nil, fmt.Errorf("%s: expected a skill folder holding %s", path, skillFile)
		default:
			return nil, fmt.Errorf("%s: expected a %s file", path, l.extension)
		}
		item, found, err := loadItem(dir, l, slug)
		if err != nil {
			return nil, err
		}
		if found {
			items = append(items, item)
		}
	}
	return items, nil
}

func checkOwner(dir string, l layout, slug string) error {
	scripts, err := scanFolder(l.folderPath(dir, slug))
	if err != nil || len(scripts.files) == 0 {
		return err
	}
	main, err := fsx.Inspect(l.mainPath(dir, slug))
	if err != nil || main.Exists {
		return err
	}
	return fmt.Errorf("%s: has files but no %s%s beside it", l.folderPath(dir, slug), slug, l.extension)
}

func loadItem(dir string, l layout, slug string) (Item, bool, error) {
	var attachments []string
	if l.folder {
		found, err := scanFolder(l.folderPath(dir, slug))
		if err != nil {
			return Item{}, false, err
		}
		attachments = found.files
	}
	path := l.mainPath(dir, slug)
	if l.kind == KindSkill && !slices.Contains(attachments, skillFile) {
		if len(attachments) == 0 {
			return Item{}, false, nil
		}
		return Item{}, false, fmt.Errorf("%s: has files but no %s", l.folderPath(dir, slug), skillFile)
	}
	main, err := readFile(path)
	if err != nil {
		return Item{}, false, err
	}
	item, err := decodeItem(l.kind, slug, main.Data)
	if err == nil {
		err = validateSlugs(item, slug)
	}
	if err != nil {
		return Item{}, false, fmt.Errorf("%s: %w", path, err)
	}
	item.Source = path
	for _, key := range attachments {
		if l.kind == KindSkill && key == skillFile {
			continue
		}
		file, err := readFile(filepath.Join(l.folderPath(dir, slug), filepath.FromSlash(key)))
		if err != nil {
			return Item{}, false, err
		}
		if item.Files == nil {
			item.Files = map[string]File{}
		}
		item.Files[key] = file
	}
	return item, true, nil
}

func checkIDs(dir string, items []Item, opt LoadOptions) error {
	sources := map[ID]string{}
	for index, item := range items {
		if item.ID == "" {
			switch {
			case opt.AssignIDs:
				continue
			case !opt.AssignTemporaryIDs:
				return fmt.Errorf("%s: item has no %s id", item.Source, blockKey)
			}
			temporary, err := temporaryID(dir, item)
			if err != nil {
				return err
			}
			item.ID, items[index].ID = temporary, temporary
		}
		if other, taken := sources[item.ID]; taken {
			return fmt.Errorf("duplicate id %s in %s and %s", item.ID, other, item.Source)
		}
		sources[item.ID] = item.Source
	}
	return nil
}

func assignIDs(dir string, items []Item) error {
	pending := map[int][]byte{}
	for index := range items {
		if items[index].ID != "" {
			continue
		}
		items[index].ID = NewID()
		data, err := canonical(items[index])
		if err != nil {
			return fmt.Errorf("%s: %w", items[index].Source, err)
		}
		pending[index] = data
	}
	for index := range items {
		if data, found := pending[index]; found {
			if err := fsx.WriteAtomic(dir, items[index].Source, data, mainMode); err != nil {
				return err
			}
		}
	}
	return nil
}
