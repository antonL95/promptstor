package claudecode

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/mdrules"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	fileMode            = fs.FileMode(0o644)
	markdownExtension   = ".md"
	frontmatterFence    = "---\n"
	reasonNoDescription = "Claude Code skips a subagent without a description"
	reasonCredential    = "skill file %s matches the credential exclusion list"
)

type field struct {
	key   string
	value any
}

func (ex *exporter) ruleFile(it library.Item) {
	slug := library.Slug(it.Name)
	if slug == "" {
		ex.report(it, reasonNoFileName)
		return
	}
	header := []field{{pathsKey, slices.Clone(it.Scope)}}
	ex.document(it, ex.l.in(rulesDir, slug+markdownExtension), header, string(mdrules.Render([]library.Item{it})))
}

func (ex *exporter) command(it library.Item) {
	var parts []string
	for _, part := range strings.Split(it.Name, commandSeparator) {
		if slug := library.Slug(part); slug != "" {
			parts = append(parts, slug)
		}
	}
	if len(parts) == 0 {
		ex.report(it, reasonNoFileName)
		return
	}
	parts[len(parts)-1] += markdownExtension
	header := describedHeader(it, "name", pathsKey)
	ex.document(it, ex.l.in(append([]string{commandsDir}, parts...)...), header, it.Body)
}

func (ex *exporter) subagent(it library.Item) {
	slug := library.Slug(it.Name)
	switch {
	case slug == "":
		ex.report(it, reasonNoFileName)
		return
	case strings.TrimSpace(it.Description) == "":
		ex.report(it, reasonNoDescription)
		return
	}
	header := append([]field{{"name", it.Name}}, describedHeader(it, "name")...)
	ex.document(it, ex.l.in(agentsDir, slug+markdownExtension), header, it.Body)
}

func describedHeader(it library.Item, skipped ...string) []field {
	var header []field
	if it.Description != "" {
		header = append(header, field{"description", it.Description})
	}
	for _, key := range it.MetaKeys() {
		if key != "description" && !slices.Contains(skipped, key) {
			header = append(header, field{key, it.Meta[key]})
		}
	}
	return header
}

func (ex *exporter) document(it library.Item, path string, header []field, body string) {
	content, err := renderDocument(header, body)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	ex.add(plan.Unit{Path: path, Kind: manifest.UnitFile, Content: content, Mode: fileMode, Items: []library.ID{it.ID}})
}

func renderDocument(header []field, body string) ([]byte, error) {
	var buffer bytes.Buffer
	if len(header) > 0 {
		mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, f := range header {
			key, value := &yaml.Node{}, &yaml.Node{}
			key.SetString(f.key)
			if err := value.Encode(f.value); err != nil {
				return nil, fmt.Errorf("frontmatter %s: %w", f.key, err)
			}
			mapping.Content = append(mapping.Content, key, value)
		}
		buffer.WriteString(frontmatterFence)
		encoder := yaml.NewEncoder(&buffer)
		encoder.SetIndent(2)
		if err := encoder.Encode(mapping); err != nil {
			return nil, fmt.Errorf("frontmatter: %w", err)
		}
		if err := encoder.Close(); err != nil {
			return nil, fmt.Errorf("frontmatter: %w", err)
		}
		buffer.WriteString(frontmatterFence)
	}
	buffer.WriteString(body)
	return managed.WithNotice(buffer.Bytes()), nil
}

func (ex *exporter) skill(it library.Item) {
	units, err := adapter.SkillUnits(ex.t, tool, it)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	for _, u := range units {
		if backup.Excluded(ex.t.Env, u.Path) {
			relative, _ := filepath.Rel(filepath.Dir(units[0].Path), u.Path)
			ex.report(it, fmt.Sprintf(reasonCredential, filepath.ToSlash(relative)))
			return
		}
	}
	ex.units = append(ex.units, units...)
}
