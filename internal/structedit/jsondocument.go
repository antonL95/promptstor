package structedit

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/tailscale/hujson"
)

const defaultIndentUnit = "  "

type jsonDocument struct {
	root    hujson.Value
	unit    string
	newline string
	compact bool
}

func parseDocument(doc []byte) (*jsonDocument, error) {
	root, err := hujson.Parse(slices.Clone(doc))
	if err != nil {
		return nil, jsonSyntaxError(err)
	}
	object, isObject := root.Value.(*hujson.Object)
	return &jsonDocument{
		root:    root,
		unit:    indentUnit(doc),
		newline: newlineOf(doc),
		compact: !bytes.Contains(bytes.TrimSpace(doc), []byte("\n")) && isObject && len(object.Members) > 0,
	}, nil
}

func jsonSyntaxError(err error) error {
	var line, column int
	if _, scanErr := fmt.Sscanf(err.Error(), "hujson: line %d, column %d", &line, &column); scanErr != nil {
		return errors.New("invalid JSON")
	}
	return fmt.Errorf("invalid JSON at line %d, column %d", line, column)
}

func (d *jsonDocument) pack() ([]byte, error) {
	out := d.root.Pack()
	if _, err := hujson.Parse(slices.Clone(out)); err != nil {
		return nil, fmt.Errorf("edit produced a broken document: %w", jsonSyntaxError(err))
	}
	return out, nil
}

func newlineOf(doc []byte) string {
	if bytes.Contains(doc, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

func indentUnit(doc []byte) string {
	for line := range strings.Lines(string(doc)) {
		content := strings.TrimLeft(line, " \t")
		indent := line[:len(line)-len(content)]
		if indent != "" && strings.TrimSpace(content) != "" && !strings.HasPrefix(content, "*") {
			return indent
		}
	}
	return defaultIndentUnit
}

func pointerSegments(pointer string) []string {
	if pointer == "" {
		return nil
	}
	unescape := strings.NewReplacer("~1", "/", "~0", "~")
	segments := strings.Split(pointer[1:], "/")
	for i := range segments {
		segments[i] = unescape.Replace(segments[i])
	}
	return segments
}

func joinPointer(segments []string) string {
	escape := strings.NewReplacer("~", "~0", "/", "~1")
	var pointer strings.Builder
	for _, segment := range segments {
		pointer.WriteString("/" + escape.Replace(segment))
	}
	return pointer.String()
}

func describePointer(segments []string) string {
	if len(segments) == 0 {
		return "the document root"
	}
	return joinPointer(segments)
}

func (d *jsonDocument) find(segments []string) (*hujson.Value, int, error) {
	current := &d.root
	for found, segment := range segments {
		index, ok, err := childIndex(current.Value, segment)
		if err != nil {
			return nil, 0, fmt.Errorf("%s: %w", joinPointer(segments[:found+1]), err)
		}
		if !ok {
			return current, found, nil
		}
		current = childAt(current.Value, index)
	}
	return current, len(segments), nil
}

func childIndex(composite hujson.ValueTrimmed, segment string) (int, bool, error) {
	switch typed := composite.(type) {
	case *hujson.Object:
		return memberIndex(typed, segment)
	case *hujson.Array:
		index, err := strconv.Atoi(segment)
		ok := err == nil && index >= 0 && index < len(typed.Elements) && strconv.Itoa(index) == segment
		return index, ok, nil
	default:
		return 0, false, nil
	}
}

func memberIndex(object *hujson.Object, name string) (int, bool, error) {
	index, matches := 0, 0
	for i, member := range object.Members {
		if literal, ok := member.Name.Value.(hujson.Literal); ok && literal.String() == name {
			index = i
			matches++
		}
	}
	if matches > 1 {
		return 0, false, errors.New("member name appears more than once")
	}
	return index, matches == 1, nil
}

func packBare(value *hujson.Value) []byte {
	return hujson.Value{Value: value.Value}.Pack()
}

func matchingElements(array *hujson.Array, key manifest.Key) ([]int, error) {
	var matches []int
	for i := range array.Elements {
		ok, err := elementMatches(&array.Elements[i], key)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		if ok {
			matches = append(matches, i)
		}
	}
	return matches, nil
}

func checkSelected(value []byte, key manifest.Key) error {
	parsed, err := hujson.Parse(slices.Clone(value))
	if err != nil {
		return jsonSyntaxError(err)
	}
	matches, err := elementMatches(&parsed, key)
	if err != nil {
		return fmt.Errorf("check value against selector: %w", err)
	}
	if !matches {
		return errors.New("value does not match the selector of the key")
	}
	return nil
}

func elementMatches(element *hujson.Value, key manifest.Key) (bool, error) {
	switch key.Form {
	case manifest.KeyFormScalar:
		return isString(element, key.Value), nil
	case manifest.KeyFormMatch:
		return subMatches(element, key)
	case manifest.KeyFormHash:
		hash, err := manifest.Hash(manifest.UnitJSONElement, packBare(element))
		return hash == key.Hash, err
	default:
		return false, errors.New("key does not select an array element")
	}
}

func subMatches(element *hujson.Value, key manifest.Key) (bool, error) {
	if _, ok := element.Value.(*hujson.Object); !ok {
		return false, nil
	}
	nested := jsonDocument{root: *element}
	segments := pointerSegments(key.Sub)
	target, found, err := nested.find(segments)
	if err != nil || found < len(segments) {
		return false, err
	}
	return isString(target, key.Value), nil
}

func isString(value *hujson.Value, want string) bool {
	literal, ok := value.Value.(hujson.Literal)
	return ok && literal.Kind() == '"' && literal.String() == want
}
