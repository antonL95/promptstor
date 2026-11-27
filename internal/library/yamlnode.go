package library

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

type field struct{ key, value *yaml.Node }

func parseMapping(data []byte) ([]field, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, parseError(err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("holds more than one YAML document")
	}
	root := document.Content[0]
	if hasAlias(root) {
		return nil, errors.New("YAML alias is not supported")
	}
	return mappingFields(root, "the document")
}

func parseError(err error) error {
	if strings.Contains(err.Error(), "anchor") {
		return errors.New("invalid YAML: unknown alias")
	}
	return fmt.Errorf("invalid YAML: %w", err)
}

func hasAlias(node *yaml.Node) bool {
	if node.Kind == yaml.AliasNode {
		return true
	}
	for _, child := range node.Content {
		if hasAlias(child) {
			return true
		}
	}
	return false
}

func isNull(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.ShortTag() == "!!null"
}

func mappingFields(node *yaml.Node, what string) ([]field, error) {
	if isNull(node) {
		return nil, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s on line %d is not a mapping", what, node.Line)
	}
	fields := make([]field, 0, len(node.Content)/2)
	seen := map[string]bool{}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key, value := node.Content[index], node.Content[index+1]
		if key.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("%s has a key on line %d that is not a string", what, key.Line)
		}
		if seen[key.Value] {
			return nil, fmt.Errorf("duplicate key %q on line %d", key.Value, key.Line)
		}
		seen[key.Value] = true
		fields = append(fields, field{key, value})
	}
	return fields, nil
}

func decodeValue(f field, target any) error {
	if err := f.value.Decode(target); err != nil {
		return fmt.Errorf("field %q on line %d has the wrong type or a duplicate key", f.key.Value, f.key.Line)
	}
	return nil
}

func decodeKnown(node *yaml.Node, what string, targets map[string]any) error {
	fields, err := mappingFields(node, what)
	if err != nil {
		return err
	}
	for _, f := range fields {
		target, known := targets[f.key.Value]
		if !known {
			return fmt.Errorf("unknown key %q in %s", f.key.Value, what)
		}
		if err := decodeValue(f, target); err != nil {
			return err
		}
	}
	return nil
}

type mappingBuilder struct {
	node yaml.Node
	err  error
}

func newMapping() *mappingBuilder {
	return &mappingBuilder{node: yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}}
}

func stringNode(value string) *yaml.Node {
	node := &yaml.Node{}
	node.SetString(value)
	return node
}

func (m *mappingBuilder) addNodes(key, value *yaml.Node) {
	m.node.Content = append(m.node.Content, key, value)
}

func (m *mappingBuilder) add(key string, value any) {
	node, ok := value.(*yaml.Node)
	if !ok {
		node = &yaml.Node{}
		if err := node.Encode(value); err != nil {
			m.err = errors.Join(m.err, fmt.Errorf("encode %q: %w", key, err))
			return
		}
	}
	m.addNodes(stringNode(key), node)
}

func (m *mappingBuilder) addString(key, value string) {
	if value != "" {
		m.addNodes(stringNode(key), stringNode(value))
	}
}

func (m *mappingBuilder) addList(key string, values []string) {
	if len(values) > 0 {
		m.add(key, values)
	}
}

func (m *mappingBuilder) empty() bool { return len(m.node.Content) == 0 }

func (m *mappingBuilder) bytes() ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.empty() {
		return nil, nil
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(&m.node); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
