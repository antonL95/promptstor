package codex

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/antonL95/promptstor/internal/library"
)

const (
	eventField      = "event"
	matcherField    = "matcher"
	hooksKey        = "hooks"
	hookStateKey    = "state"
	permissionsKey  = "permissions"
	agentRolesKey   = "agents"
	noteInlineHooks = "inline [hooks]; not imported"
	noteProfileName = "permission profile %s; not imported"
	noteInlineRole  = "inline agent role %s; not imported"
)

func (im *importer) hooks() error {
	path := im.l.in(hooksName)
	data, found, err := im.read(path)
	if err != nil || !found {
		return err
	}
	var doc struct {
		Hooks map[string][]map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		im.note(path, noteUnreadable, err)
		return nil
	}
	used := map[string]int{}
	for _, event := range slices.Sorted(maps.Keys(doc.Hooks)) {
		for _, group := range doc.Hooks[event] {
			native := maps.Clone(group)
			native[eventField] = event
			name := event
			if matcher, _ := group[matcherField].(string); matcher != "" {
				name += " " + matcher
			}
			if used[name]++; used[name] > 1 {
				name = fmt.Sprintf("%s %d", name, used[name])
			}
			im.out.Items = append(im.out.Items, library.Item{
				Kind:   library.KindHook,
				Name:   name,
				Source: path,
				Native: map[string]any{tool: native},
			})
		}
	}
	return nil
}

func (im *importer) config() error {
	path := im.l.in(configName)
	data, found, err := im.read(path)
	if err != nil || !found {
		return err
	}
	doc, ok := decodeTable(data)
	if !ok {
		im.note(path, noteInvalidTOML)
		return nil
	}
	for key := range subtable(doc, hooksKey) {
		if key != hookStateKey {
			im.note(path, noteInlineHooks)
			break
		}
	}
	for _, name := range slices.Sorted(maps.Keys(subtable(doc, permissionsKey))) {
		if name != profileName {
			im.note(path, noteProfileName, name)
		}
	}
	roles := subtable(doc, agentRolesKey)
	for _, name := range slices.Sorted(maps.Keys(roles)) {
		if _, role := roles[name].(map[string]any); role {
			im.note(path, noteInlineRole, name)
		}
	}
	im.servers(path, subtable(doc, serversKey))
	return nil
}
