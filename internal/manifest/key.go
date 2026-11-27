package manifest

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type KeyForm int

const (
	KeyFormPlain KeyForm = iota
	KeyFormMatch
	KeyFormScalar
	KeyFormHash
)

type Key struct {
	Pointer, Sub, Value, Hash string
	Form                      KeyForm
}

func ParseKey(u UnitKind, key string) (Key, error) {
	parsed, err := parseKey(u, key)
	if err != nil {
		return Key{}, fmt.Errorf("%s key %q: %w", u, key, err)
	}
	return parsed, nil
}

func parseKey(u UnitKind, key string) (Key, error) {
	switch u {
	case UnitFile, UnitSection:
		if key != "" {
			return Key{}, errors.New("must be empty")
		}
		return Key{}, nil
	case UnitJSON:
		return Key{Pointer: key}, checkPointer(key)
	case UnitJSONElement:
		return parseElementKey(key)
	case UnitTOMLTable:
		if key == "" {
			return Key{}, errors.New("needs a table path")
		}
		return Key{Pointer: key}, nil
	default:
		return Key{}, errors.New("unknown unit kind")
	}
}

func parseElementKey(key string) (Key, error) {
	pointer, selector, found := strings.Cut(key, "@")
	if !found {
		return Key{}, errors.New("needs an @ after the array pointer")
	}
	if err := checkPointer(pointer); err != nil {
		return Key{}, err
	}
	parsed, err := parseSelector(selector)
	parsed.Pointer = pointer
	return parsed, err
}

func parseSelector(selector string) (Key, error) {
	if hash, ok := strings.CutPrefix(selector, "#"); ok {
		if !isSHA256(hash) {
			return Key{}, errors.New("hash must be 64 lowercase hex characters")
		}
		return Key{Hash: hash, Form: KeyFormHash}, nil
	}
	if value, ok := strings.CutPrefix(selector, "="); ok {
		return Key{Value: value, Form: KeyFormScalar}, nil
	}
	sub, value, found := strings.Cut(selector, "=")
	if !found || !strings.HasPrefix(sub, "/") {
		return Key{}, errors.New("selector must be #hash, =value or /pointer=value")
	}
	return Key{Sub: sub, Value: value, Form: KeyFormMatch}, checkPointer(sub)
}

func isSHA256(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == 32 && hash == strings.ToLower(hash)
}

func checkPointer(pointer string) error {
	if pointer == "" {
		return nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return errors.New("pointer must be empty or start with /")
	}
	unescaped := strings.NewReplacer("~0", "", "~1", "").Replace(pointer)
	if strings.Contains(unescaped, "~") {
		return errors.New("pointer has a ~ that is not ~0 or ~1")
	}
	return nil
}
