package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/pelletier/go-toml/v2"
	"github.com/tailscale/hujson"
)

func Hash(u UnitKind, content []byte) (string, error) {
	canonical, err := canonicalise(u, content)
	if err != nil {
		return "", fmt.Errorf("hash %s unit: %w", u, err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalise(u UnitKind, content []byte) ([]byte, error) {
	switch u {
	case UnitFile, UnitSection:
		return canonicalText(content), nil
	case UnitJSON, UnitJSONElement:
		return canonicalJSON(content)
	case UnitTOMLTable:
		return canonicalTOML(content)
	default:
		return nil, errors.New("unknown unit kind")
	}
}

func canonicalText(content []byte) []byte {
	lines := bytes.Split(bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n")), []byte("\n"))
	for i := range lines {
		lines[i] = bytes.TrimRight(lines[i], " \t")
	}
	return bytes.Join(lines, []byte("\n"))
}

func canonicalJSON(content []byte) ([]byte, error) {
	standard, err := hujson.Standardize(slices.Clone(content))
	if err != nil {
		return nil, errors.New("invalid JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(standard))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("invalid JSON")
	}
	return encode(value)
}

func canonicalTOML(content []byte) ([]byte, error) {
	var table map[string]any
	if err := toml.Unmarshal(content, &table); err != nil {
		return nil, tomlError(err)
	}
	return encode(table)
}

func tomlError(err error) error {
	var decodeError *toml.DecodeError
	if errors.As(err, &decodeError) {
		row, column := decodeError.Position()
		return fmt.Errorf("invalid TOML at line %d, column %d", row, column)
	}
	return errors.New("invalid TOML")
}

func encode(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("value has no canonical JSON form")
	}
	return encoded, nil
}
