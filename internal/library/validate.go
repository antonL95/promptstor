package library

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

func validateActivation(activation Activation) error {
	switch activation {
	case "", ActivationAlways, ActivationFiles, ActivationAgent:
		return nil
	default:
		return fmt.Errorf("unknown activation %q", activation)
	}
}

func validateSlugs(it Item, slug string) error {
	if it.Kind != KindSkill && it.Kind != KindCommand {
		return nil
	}
	for _, candidate := range []string{slug, it.Name} {
		if !strictSlug.MatchString(candidate) {
			return fmt.Errorf("%s name %q must match %s", it.Kind, candidate, strictSlug)
		}
	}
	if it.Kind == KindSkill && it.Name != slug {
		return fmt.Errorf("skill name %q differs from its folder %q", it.Name, slug)
	}
	return nil
}

func validateForWrite(l layout, it Item) error {
	slug := writeSlug(it)
	if slug == "" {
		return errors.New("the name gives no file name")
	}
	if err := validateSlugs(it, slug); err != nil {
		return err
	}
	if err := validateActivation(it.Activation); err != nil {
		return err
	}
	if err := validateKindData(l, it); err != nil {
		return err
	}
	if err := validateMetaKeys(l, it); err != nil {
		return err
	}
	return validateFileKeys(it)
}

func validateKindData(l layout, it Item) error {
	switch {
	case l.isYAML() && it.Body != "":
		return fmt.Errorf("a %s cannot hold a body", it.Kind)
	case !l.folder && len(it.Files) > 0:
		return fmt.Errorf("a %s cannot hold files", it.Kind)
	case !slices.Contains(kindKeys(it.Kind), "native") && len(it.Native) > 0:
		return fmt.Errorf("a %s cannot hold native settings", it.Kind)
	case it.Kind != KindPermission && it.Portable != nil:
		return fmt.Errorf("a %s cannot hold portable permissions", it.Kind)
	case it.Kind != KindMCP && it.MCP != nil:
		return fmt.Errorf("a %s cannot hold an MCP server", it.Kind)
	}
	return validateValues(it.MCP)
}

func validateValues(server *MCPServer) error {
	if server == nil {
		return nil
	}
	for _, values := range []map[string]Value{server.Env, server.Headers} {
		for name, value := range values {
			if value.Literal != "" && value.Ref != "" {
				return fmt.Errorf("value %q has both a literal and a reference", name)
			}
		}
	}
	return nil
}

func validateMetaKeys(l layout, it Item) error {
	if l.isYAML() && len(it.Meta) > 0 {
		return fmt.Errorf("unknown keys %q for a %s", slices.Sorted(maps.Keys(it.Meta)), it.Kind)
	}
	for _, key := range []string{"name", "description", blockKey} {
		if _, found := it.Meta[key]; found {
			return fmt.Errorf("meta key %q is reserved", key)
		}
	}
	return nil
}

func validateFileKeys(it Item) error {
	for key := range it.Files {
		switch {
		case !fs.ValidPath(key) || key == "." || !filepath.IsLocal(filepath.FromSlash(key)) || strings.Contains(key, `\`):
			return fmt.Errorf("file %q is not a path inside the item folder", key)
		case it.Kind == KindSkill && strings.EqualFold(key, skillFile):
			return fmt.Errorf("file %q is the main file", key)
		case slices.ContainsFunc(strings.Split(key, "/"), isHidden):
			return fmt.Errorf("file %q would be ignored on load", key)
		}
	}
	return nil
}
