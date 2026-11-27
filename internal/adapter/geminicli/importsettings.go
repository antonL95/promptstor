package geminicli

import (
	"fmt"
	"maps"
	"slices"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
)

const (
	hooksKey            = "hooks"
	eventField          = "event"
	matcherField        = "matcher"
	toolsKey            = "tools"
	mcpKey              = "mcp"
	allowedKey          = "allowed"
	excludeKey          = "exclude"
	allowedServers      = "/mcp/allowed"
	allowedTools        = "/tools/allowed"
	noteInvalidSettings = "invalid JSON; not imported"
	noteEventShape      = "hooks/%s is not a list of hook groups; not imported"
	noteToolsExclude    = "tools.exclude is deprecated; not imported"
)

func (im *importer) settings() error {
	doc, valid, err := readSettings(im.l.settings)
	if err != nil {
		return err
	}
	if !valid {
		im.note(im.l.settings, noteInvalidSettings)
		return nil
	}
	if im.l.scope == adapter.ScopeProject && userSetContextFiles(doc) {
		im.note(im.l.settings, reasonUserFileName)
	}
	im.grants(allowedServers, textList(object(doc, mcpKey)[allowedKey]))
	im.grants(allowedTools, textList(object(doc, toolsKey)[allowedKey]))
	if len(textList(object(doc, toolsKey)[excludeKey])) > 0 {
		im.note(im.l.settings, noteToolsExclude)
	}
	im.hooks(object(doc, hooksKey))
	im.servers(object(doc, serversKey))
	return nil
}

func (im *importer) grants(pointer string, entries []string) {
	for _, entry := range entries {
		key, err := adapter.ElementKey(pointer, "", entry)
		if err != nil {
			im.note(im.l.settings, "%s entry %q: %v", pointer, entry, err)
			continue
		}
		im.out.Grants = append(im.out.Grants, adapter.Grant{Tool: tool, Path: im.l.settings, Key: key, Value: entry})
	}
}

func (im *importer) hooks(events map[string]any) {
	used := map[string]int{}
	for _, event := range slices.Sorted(maps.Keys(events)) {
		groups, ok := events[event].([]any)
		if !ok {
			im.note(im.l.settings, noteEventShape, event)
			continue
		}
		for _, entry := range groups {
			group, ok := entry.(map[string]any)
			if !ok {
				im.note(im.l.settings, noteEventShape, event)
				continue
			}
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
				Source: im.l.settings,
				Native: map[string]any{tool: native},
			})
		}
	}
}
