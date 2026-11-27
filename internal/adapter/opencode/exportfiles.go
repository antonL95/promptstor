package opencode

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	frontmatterFence     = "---\n"
	reasonDropped        = "written without %s, which opencode %s files do not take"
	reasonNoPrompt       = "the command has no prompt"
	reasonText           = "the item is not valid UTF-8"
	reasonCredentialPath = "%s matches the credential exclusion list"
	reasonBuiltinAgent   = "%q is a built in opencode agent name"
	reasonDefined        = "%s already defines this name; not written"
)

func (ex *exporter) skill(it library.Item) {
	units, err := adapter.SkillUnits(ex.t, tool, it)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	for _, u := range units {
		if backup.Excluded(ex.t.Env, u.Path) {
			relative, _ := filepath.Rel(filepath.Join(ex.l.sharedSkills(), it.Name), u.Path)
			ex.report(it, fmt.Sprintf(reasonCredentialPath, "skill file "+filepath.ToSlash(relative)))
			return
		}
	}
	ex.units = append(ex.units, units...)
}

func slugParts(name, separator string) []string {
	var parts []string
	for _, part := range strings.Split(name, separator) {
		if slug := library.Slug(part); slug != "" {
			parts = append(parts, slug)
		}
	}
	return parts
}

func (ex *exporter) definedElsewhere(it library.Item, parts, dirs []string) bool {
	file := filepath.Join(parts...) + markdownExtension
	for _, dir := range dirs {
		if exists(ex.l.in(dir, file)) {
			ex.report(it, fmt.Sprintf(reasonDefined, dir+"/"+filepath.ToSlash(file)))
			return true
		}
	}
	return false
}

func (ex *exporter) command(it library.Item) {
	parts := slugParts(it.Name, commandSeparator)
	switch {
	case len(parts) == 0:
		ex.report(it, reasonNoFileName)
		return
	case strings.TrimSpace(it.Body) == "":
		ex.report(it, reasonNoPrompt)
		return
	case ex.definedElsewhere(it, parts, []string{legacyCommandsDir}):
		return
	}
	header := newHeader()
	if it.Description != "" {
		header.set(descriptionKey, it.Description)
	}
	written := []string{nameKey, descriptionKey}
	for _, f := range commandFields {
		if value, present := it.Meta[f.meta]; present && f.accepts(value) {
			header.set(f.name, value)
			written = append(written, f.meta)
		}
	}
	ex.document(it, commandsDir, parts, header, written, "command")
}

func (ex *exporter) subagent(it library.Item) {
	parts := slugParts(it.Name, agentSeparator)
	id := strings.Join(parts, agentSeparator)
	switch {
	case len(parts) == 0:
		ex.report(it, reasonNoFileName)
		return
	case slices.Contains(builtinAgents, id):
		ex.report(it, fmt.Sprintf(reasonBuiltinAgent, id))
		return
	case ex.definedElsewhere(it, parts, []string{legacyAgentsDir, legacyModesDir, modesDir}):
		return
	}
	header := newHeader()
	if it.Description != "" {
		header.set(descriptionKey, it.Description)
	}
	written := []string{nameKey, descriptionKey}
	for _, f := range agentFields {
		value, present := it.Meta[f.meta]
		accepted := present && f.accepts(value)
		switch {
		case accepted:
			written = append(written, f.meta)
		case f.meta == modeMeta:
			value = subagentMode
		default:
			continue
		}
		if rules, ok := rulesetOf(value); ok {
			value = rules
		}
		header.set(f.name, value)
	}
	ex.document(it, agentsDir, parts, header, written, "agent")
}

func (ex *exporter) document(it library.Item, dir string, parts []string, h *header, written []string, kind string) {
	if !utf8.ValidString(it.Description + it.Body) {
		ex.report(it, reasonText)
		return
	}
	content, err := h.markdown(it.Body)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	path := ex.l.in(dir, filepath.Join(parts...)+markdownExtension)
	if backup.Excluded(ex.t.Env, path) {
		ex.report(it, fmt.Sprintf(reasonCredentialPath, filepath.Base(path)))
		return
	}
	ex.add(plan.Unit{Path: path, Kind: manifest.UnitFile, Content: content, Items: []library.ID{it.ID}})
	dropped := slices.DeleteFunc(it.MetaKeys(), func(key string) bool { return slices.Contains(written, key) })
	if len(dropped) > 0 {
		slices.Sort(dropped)
		ex.report(it, fmt.Sprintf(reasonDropped, strings.Join(dropped, ", "), kind))
	}
}

type header struct {
	node yaml.Node
	err  error
}

func newHeader() *header {
	return &header{node: yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}}
}

func (h *header) set(key string, value any) {
	keyNode, valueNode := &yaml.Node{}, &yaml.Node{}
	keyNode.SetString(key)
	if err := valueNode.Encode(value); err != nil {
		if h.err == nil {
			h.err = fmt.Errorf("frontmatter %s: %w", key, err)
		}
		return
	}
	h.node.Content = append(h.node.Content, keyNode, valueNode)
}

func (h *header) markdown(body string) ([]byte, error) {
	if h.err != nil {
		return nil, h.err
	}
	var buffer bytes.Buffer
	buffer.WriteString(frontmatterFence)
	if len(h.node.Content) > 0 {
		encoder := yaml.NewEncoder(&buffer)
		encoder.SetIndent(2)
		if err := encoder.Encode(&h.node); err != nil {
			return nil, fmt.Errorf("frontmatter: %w", err)
		}
		if err := encoder.Close(); err != nil {
			return nil, fmt.Errorf("frontmatter: %w", err)
		}
	}
	buffer.WriteString(frontmatterFence)
	buffer.WriteString(body)
	return managed.WithNotice(buffer.Bytes()), nil
}
