package library

import (
	"maps"
	"reflect"
	"slices"
)

func encodeItem(it Item) ([]byte, error) {
	l, err := layoutOf(it.Kind)
	if err != nil {
		return nil, err
	}
	mapping := newMapping()
	if l.isYAML() {
		it.addBlock(mapping)
		it.addIdentity(mapping)
		it.addKindFields(mapping)
		it.addMeta(mapping)
		return mapping.bytes()
	}
	it.addIdentity(mapping)
	it.addBlock(mapping)
	it.addMeta(mapping)
	header, err := mapping.bytes()
	if err != nil {
		return nil, err
	}
	return joinFrontmatter(header, it.Body), nil
}

func (it Item) addIdentity(mapping *mappingBuilder) {
	if it.nameInFile || it.Name != writeSlug(it) {
		mapping.addString("name", it.Name)
	}
	mapping.addString("description", it.Description)
}

func (it Item) addBlock(mapping *mappingBuilder) {
	value := block{
		ID:         it.ID,
		Order:      it.Order,
		Scope:      it.Scope,
		Activation: it.Activation,
		Locked:     it.Locked,
		Removed:    it.Removed,
	}
	if len(it.Targets.Include) > 0 || len(it.Targets.Exclude) > 0 {
		value.Targets = &blockTargets{Include: it.Targets.Include, Exclude: it.Targets.Exclude}
	}
	if !isEmptyBlock(value) {
		mapping.add(blockKey, value)
	}
}

func isEmptyBlock(value block) bool {
	if len(value.Scope) == 0 {
		value.Scope = nil
	}
	return reflect.ValueOf(value).IsZero()
}

func (it Item) addKindFields(mapping *mappingBuilder) {
	if p := it.Portable; p != nil {
		mapping.addList("deny_commands", p.DenyCommands)
		mapping.addList("deny_paths", p.DenyPaths)
		mapping.addList("allow_mcp_servers", p.AllowMCPServers)
	}
	if len(it.Native) > 0 {
		mapping.add("native", it.Native)
	}
	if s := it.MCP; s != nil {
		mapping.addString("transport", s.Transport)
		mapping.addString("command", s.Command)
		mapping.addList("args", s.Args)
		mapping.addString("url", s.URL)
		if len(s.Env) > 0 {
			mapping.add("env", encodeValues(s.Env))
		}
		if len(s.Headers) > 0 {
			mapping.add("headers", encodeValues(s.Headers))
		}
		if s.Resolve {
			mapping.add("resolve", true)
		}
	}
}

func (it Item) addMeta(mapping *mappingBuilder) {
	written := map[string]bool{}
	for _, original := range it.metaOrder {
		key := original.key.Value
		value, present := it.Meta[key]
		if !present {
			continue
		}
		written[key] = true
		var unchanged any
		if original.value.Decode(&unchanged) == nil && reflect.DeepEqual(unchanged, value) {
			mapping.addNodes(original.key, original.value)
			continue
		}
		mapping.add(key, value)
	}
	for _, key := range slices.Sorted(maps.Keys(it.Meta)) {
		if !written[key] {
			mapping.add(key, it.Meta[key])
		}
	}
}
