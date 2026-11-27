package adapter

import (
	"fmt"

	"github.com/antonL95/promptstor/internal/manifest"
)

func ElementKey(pointer, sub, value string) (string, error) {
	key := pointer + "@" + sub + "=" + value
	want := manifest.Key{Pointer: pointer, Sub: sub, Value: value, Form: manifest.KeyFormMatch}
	if sub == "" {
		want.Form = manifest.KeyFormScalar
	}
	parsed, err := manifest.ParseKey(manifest.UnitJSONElement, key)
	if err != nil {
		return "", fmt.Errorf("element key for %s: %w", pointer, err)
	}
	if parsed != want {
		return "", fmt.Errorf("element key %q does not parse back to pointer %q, sub %q and its value", key, pointer, sub)
	}
	return key, nil
}
