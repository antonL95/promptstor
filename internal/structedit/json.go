package structedit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/tailscale/hujson"
)

func JSONGet(doc []byte, key string) ([]byte, bool, error) {
	got, found, err := readKey(doc, key)
	if err != nil {
		return nil, false, fmt.Errorf("get JSON key %q: %w", key, err)
	}
	return got, found, nil
}

func JSONSet(doc []byte, key string, value []byte) ([]byte, error) {
	out, err := writeKey(doc, key, value)
	if err != nil {
		return nil, fmt.Errorf("set JSON key %q: %w", key, err)
	}
	return out, nil
}

func JSONDelete(doc []byte, key string) ([]byte, error) {
	out, err := removeKey(doc, key)
	if err != nil {
		return nil, fmt.Errorf("delete JSON key %q: %w", key, err)
	}
	return out, nil
}

type jsonKey struct {
	manifest.Key
	segments []string
	element  bool
}

func parseJSONKey(key string) (jsonKey, error) {
	unit := manifest.UnitJSON
	element := strings.Contains(key, "@")
	if element {
		unit = manifest.UnitJSONElement
	}
	parsed, err := manifest.ParseKey(unit, key)
	if err != nil {
		return jsonKey{}, err
	}
	return jsonKey{Key: parsed, segments: pointerSegments(parsed.Pointer), element: element}, nil
}

func parseEditKey(key string) (jsonKey, error) {
	parsed, err := parseJSONKey(key)
	if err == nil && !parsed.element && parsed.Pointer == "" {
		err = errors.New("refusing to change the whole document")
	}
	return parsed, err
}

func readKey(doc []byte, key string) ([]byte, bool, error) {
	parsed, err := parseJSONKey(key)
	if err != nil || isBlank(doc) {
		return nil, false, err
	}
	document, err := parseDocument(doc)
	if err != nil {
		return nil, false, err
	}
	target, found, err := document.find(parsed.segments)
	if err != nil || found < len(parsed.segments) {
		return nil, false, err
	}
	if !parsed.element {
		return packBare(target), true, nil
	}
	array, ok := target.Value.(*hujson.Array)
	if !ok {
		return nil, false, nil
	}
	matches, err := matchingElements(array, parsed.Key)
	if err != nil || len(matches) == 0 {
		return nil, false, err
	}
	return packBare(&array.Elements[matches[0]]), true, nil
}

func writeKey(doc []byte, key string, value []byte) ([]byte, error) {
	parsed, err := parseEditKey(key)
	if err != nil {
		return nil, err
	}
	if !json.Valid(value) || !utf8.Valid(value) {
		return nil, errors.New("value is not strict JSON")
	}
	source := doc
	if isBlank(doc) {
		source = []byte("{}\n")
	}
	document, err := parseDocument(source)
	if err != nil {
		return nil, err
	}
	if parsed.element {
		err = document.setElement(parsed, value)
	} else {
		err = document.set(parsed.segments, value)
	}
	if err != nil {
		return nil, err
	}
	return document.pack()
}

func removeKey(doc []byte, key string) ([]byte, error) {
	parsed, err := parseEditKey(key)
	if err != nil || isBlank(doc) {
		return doc, err
	}
	document, err := parseDocument(doc)
	if err != nil {
		return nil, err
	}
	var changed bool
	if parsed.element {
		changed, err = document.deleteElements(parsed)
	} else {
		changed, err = document.deleteMember(parsed.segments)
	}
	if err != nil || !changed {
		return doc, err
	}
	return document.pack()
}

func (d *jsonDocument) set(segments []string, value []byte) error {
	parentSegments := segments[:len(segments)-1]
	target, found, err := d.find(segments)
	if err != nil {
		return err
	}
	if found == len(segments) {
		parent, _, err := d.find(parentSegments)
		if err != nil {
			return err
		}
		index, _, err := childIndex(parent.Value, segments[found-1])
		if err != nil {
			return err
		}
		return d.replace(parent.Value, index, len(parentSegments), value)
	}
	name := segments[found]
	switch composite := target.Value.(type) {
	case *hujson.Object:
	case *hujson.Array:
		if name != "-" && name != fmt.Sprint(len(composite.Elements)) {
			return fmt.Errorf("index %s is outside the array", joinPointer(segments[:found+1]))
		}
	default:
		return fmt.Errorf("%s is not an object or array", describePointer(segments[:found]))
	}
	return d.insert(target.Value, found, name, nest(segments[found+1:], value))
}

func (d *jsonDocument) setElement(key jsonKey, value []byte) error {
	target, found, err := d.find(key.segments)
	if err != nil {
		return err
	}
	if found < len(key.segments) {
		return d.set(key.segments, slices.Concat([]byte("["), value, []byte("]")))
	}
	array, ok := target.Value.(*hujson.Array)
	if !ok {
		return fmt.Errorf("%s is not an array", describePointer(key.segments))
	}
	matches, err := matchingElements(array, key.Key)
	if err != nil {
		return err
	}
	if len(matches) > 0 {
		return d.replace(array, matches[0], found, value)
	}
	return d.insert(array, found, "", value)
}

func (d *jsonDocument) deleteMember(segments []string) (bool, error) {
	parentSegments := segments[:len(segments)-1]
	parent, found, err := d.find(parentSegments)
	if err != nil || found < len(parentSegments) {
		return false, err
	}
	index, ok, err := childIndex(parent.Value, segments[found])
	if err != nil || !ok {
		return false, err
	}
	removeChild(parent.Value, index)
	return true, nil
}

func (d *jsonDocument) deleteElements(key jsonKey) (bool, error) {
	target, found, err := d.find(key.segments)
	if err != nil || found < len(key.segments) {
		return false, err
	}
	array, ok := target.Value.(*hujson.Array)
	if !ok {
		return false, nil
	}
	matches, err := matchingElements(array, key.Key)
	if err != nil {
		return false, err
	}
	for _, index := range slices.Backward(matches) {
		removeChild(array, index)
	}
	return len(matches) > 0, nil
}

func nest(segments []string, value []byte) []byte {
	for _, segment := range slices.Backward(segments) {
		value = slices.Concat([]byte("{"), hujson.String(segment), []byte(":"), value, []byte("}"))
	}
	return value
}

func isBlank(doc []byte) bool {
	return len(bytes.TrimSpace(doc)) == 0
}
