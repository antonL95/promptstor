package library

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/antonL95/promptstor/internal/fsx"
)

const (
	temporaryPrefix = "ps_tmp_"
	temporaryLength = 26
)

type Assigned struct {
	Renamed map[ID]ID
	Linked  string
}

func IsTemporary(id ID) bool {
	return strings.HasPrefix(string(id), temporaryPrefix)
}

func temporaryID(dir string, it Item) (ID, error) {
	relative, err := filepath.Rel(dir, it.Source)
	if err != nil {
		return "", fmt.Errorf("%s: %w", it.Source, err)
	}
	sum := sha256.Sum256([]byte(string(it.Layer) + ":" + filepath.ToSlash(relative)))
	encoded := strings.ToLower(base32.StdEncoding.EncodeToString(sum[:]))
	return ID(temporaryPrefix + encoded[:temporaryLength]), nil
}

func WriteMissingIDs(root, dir string, layer Layer) (Assigned, error) {
	before, err := LoadLayer(dir, layer, LoadOptions{AssignTemporaryIDs: true})
	if err != nil {
		return Assigned{}, err
	}
	missing := map[string]ID{}
	for _, it := range before {
		if IsTemporary(it.ID) {
			missing[it.Source] = it.ID
		}
	}
	if len(missing) == 0 {
		return Assigned{}, nil
	}
	link, linked, err := fsx.LinkedAncestor(root, dir)
	if err != nil {
		return Assigned{}, fmt.Errorf("write ids into %s: %w", dir, err)
	}
	if linked {
		return Assigned{Linked: link}, nil
	}
	after, err := LoadLayer(dir, layer, LoadOptions{AssignIDs: true})
	if err != nil {
		return Assigned{}, err
	}
	renamed := map[ID]ID{}
	for _, it := range after {
		if temporary, found := missing[it.Source]; found && temporary != it.ID {
			renamed[temporary] = it.ID
		}
	}
	return Assigned{Renamed: renamed}, nil
}
