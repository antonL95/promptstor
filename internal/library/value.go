package library

import (
	"fmt"
	"maps"
	"slices"

	"go.yaml.in/yaml/v3"
)

const refKey = "ref"

func decodeValues(f field) (map[string]Value, error) {
	entries, err := mappingFields(f.value, fmt.Sprintf("field %q", f.key.Value))
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	values := make(map[string]Value, len(entries))
	for _, entry := range entries {
		value, ok := decodeSecretValue(entry.value)
		if !ok {
			return nil, fmt.Errorf("%s %q on line %d must be a string or {ref: ...}", f.key.Value, entry.key.Value, entry.key.Line)
		}
		values[entry.key.Value] = value
	}
	return values, nil
}

func decodeSecretValue(node *yaml.Node) (Value, bool) {
	if node.Kind == yaml.ScalarNode && !isNull(node) {
		return Value{Literal: node.Value}, true
	}
	if node.Kind != yaml.MappingNode || len(node.Content) != 2 {
		return Value{}, false
	}
	key, ref := node.Content[0], node.Content[1]
	if key.Value != refKey || ref.Kind != yaml.ScalarNode || isNull(ref) || ref.Value == "" {
		return Value{}, false
	}
	return Value{Ref: ref.Value}, true
}

func encodeValues(values map[string]Value) *yaml.Node {
	mapping := newMapping()
	for _, name := range slices.Sorted(maps.Keys(values)) {
		value := values[name]
		if value.Ref == "" {
			mapping.addNodes(stringNode(name), stringNode(value.Literal))
			continue
		}
		ref := newMapping()
		ref.addString(refKey, value.Ref)
		mapping.addNodes(stringNode(name), &ref.node)
	}
	return &mapping.node
}
