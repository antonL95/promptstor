package backup

import (
	"fmt"
	"slices"

	"github.com/antonL95/promptstor/internal/structedit"
)

const redactedValue = `"<promptstor:redacted>"`

func sanitize(doc []byte, excluded, redact []string) ([]byte, []string, error) {
	for _, pointer := range excluded {
		var err error
		if doc, err = structedit.JSONDelete(doc, pointer); err != nil {
			return nil, nil, err
		}
	}
	var redacted []string
	for _, key := range sortedSet(redact) {
		if pointerExcluded(excluded, key) {
			continue
		}
		_, found, err := structedit.JSONGet(doc, key)
		if err != nil {
			return nil, nil, err
		}
		if !found {
			continue
		}
		if doc, err = structedit.JSONSet(doc, key, []byte(redactedValue)); err != nil {
			return nil, nil, err
		}
		redacted = append(redacted, key)
	}
	return doc, redacted, nil
}

func extractKeys(doc []byte, keys, excluded []string) (map[string]string, error) {
	stored := make(map[string]string)
	for _, key := range keys {
		if pointerExcluded(excluded, key) {
			continue
		}
		value, found, err := structedit.JSONGet(doc, key)
		if err != nil {
			return nil, err
		}
		stored[key] = ""
		if found {
			stored[key] = string(value)
		}
	}
	return stored, nil
}

func coveredBy(stored map[string]string, keys []string) []string {
	var covered []string
	for _, key := range keys {
		for pointer, value := range stored {
			if value != "" && pointer != "" && covers(pointer, key) {
				covered = append(covered, key)
				break
			}
		}
	}
	return covered
}

func carryOver(doc, current []byte, pointers []string) ([]byte, error) {
	for _, pointer := range pointers {
		value, found, err := structedit.JSONGet(current, pointer)
		if err != nil {
			return nil, fmt.Errorf("keep %s as it is: %w", pointer, err)
		}
		if doc, err = structedit.JSONDelete(doc, pointer); err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		if doc, err = structedit.JSONSet(doc, pointer, value); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

func sortedSet(values []string) []string {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}
