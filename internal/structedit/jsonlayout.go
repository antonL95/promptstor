package structedit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/tailscale/hujson"
)

const (
	horizontalSpace = " \t\r"
	allSpace        = " \t\r\n"
)

func childCount(composite hujson.ValueTrimmed) int {
	switch typed := composite.(type) {
	case *hujson.Object:
		return len(typed.Members)
	case *hujson.Array:
		return len(typed.Elements)
	default:
		return 0
	}
}

func childAt(composite hujson.ValueTrimmed, i int) *hujson.Value {
	if object, ok := composite.(*hujson.Object); ok {
		return &object.Members[i].Value
	}
	return &composite.(*hujson.Array).Elements[i]
}

func leadAt(composite hujson.ValueTrimmed, i int) *hujson.Extra {
	if object, ok := composite.(*hujson.Object); ok {
		return &object.Members[i].Name.BeforeExtra
	}
	return &composite.(*hujson.Array).Elements[i].BeforeExtra
}

func closingExtra(composite hujson.ValueTrimmed) *hujson.Extra {
	if object, ok := composite.(*hujson.Object); ok {
		return &object.AfterExtra
	}
	return &composite.(*hujson.Array).AfterExtra
}

func isMultiline(composite hujson.ValueTrimmed) bool {
	for i := range childCount(composite) {
		if bytes.Contains(*leadAt(composite, i), []byte("\n")) {
			return true
		}
	}
	return bytes.Contains(*closingExtra(composite), []byte("\n"))
}

func lineIndent(extra hujson.Extra) (string, bool) {
	at := bytes.LastIndexByte(extra, '\n')
	if at < 0 || len(bytes.Trim(extra[at+1:], horizontalSpace)) > 0 {
		return "", false
	}
	return string(extra[at+1:]), true
}

func beforeLastLine(extra hujson.Extra) []byte {
	if _, ok := lineIndent(extra); ok {
		return bytes.TrimSuffix(extra[:bytes.LastIndexByte(extra, '\n')], []byte("\r"))
	}
	return bytes.TrimRight(extra, allSpace)
}

func hasComment(extra hujson.Extra) bool {
	return len(bytes.Trim(extra, allSpace)) > 0
}

func (d *jsonDocument) childIndent(composite hujson.ValueTrimmed, i, depth int) string {
	if i >= 0 {
		if indent, ok := lineIndent(*leadAt(composite, i)); ok {
			return indent
		}
	}
	return strings.Repeat(d.unit, depth+1)
}

func (d *jsonDocument) format(value []byte, inline bool, indent string) (hujson.Value, error) {
	var formatted bytes.Buffer
	if err := json.Compact(&formatted, value); err != nil {
		return hujson.Value{}, fmt.Errorf("format value: %w", err)
	}
	if !inline {
		compacted := slices.Clone(formatted.Bytes())
		formatted.Reset()
		if err := json.Indent(&formatted, compacted, indent, d.unit); err != nil {
			return hujson.Value{}, fmt.Errorf("format value: %w", err)
		}
	}
	text := bytes.ReplaceAll(formatted.Bytes(), []byte("\n"), []byte(d.newline))
	parsed, err := hujson.Parse(text)
	if err != nil {
		return hujson.Value{}, fmt.Errorf("format value: %w", jsonSyntaxError(err))
	}
	return parsed, nil
}

func (d *jsonDocument) replace(composite hujson.ValueTrimmed, i, depth int, value []byte) error {
	target := childAt(composite, i)
	same, err := sameJSON(packBare(target), value)
	if err != nil || same {
		return err
	}
	formatted, err := d.format(value, !isMultiline(composite), d.childIndent(composite, i, depth))
	if err != nil {
		return err
	}
	target.Value = formatted.Value
	return nil
}

func sameJSON(first, second []byte) (bool, error) {
	firstHash, err := manifest.Hash(manifest.UnitJSON, first)
	if err != nil {
		return false, err
	}
	secondHash, err := manifest.Hash(manifest.UnitJSON, second)
	return firstHash == secondHash, err
}

func (d *jsonDocument) insert(composite hujson.ValueTrimmed, depth int, name string, value []byte) error {
	count := childCount(composite)
	inline := d.compact
	if count > 0 {
		inline = !isMultiline(composite)
	}
	indent := d.childIndent(composite, count-1, depth)
	added, err := d.format(value, inline, indent)
	if err != nil {
		return err
	}
	if count > 0 && childAt(composite, count-1).AfterExtra != nil {
		added.AfterExtra = hujson.Extra{}
	}
	var lead hujson.Extra
	if inline {
		lead = inlineSeparator(composite)
	} else {
		lead = d.openLine(composite, depth, indent)
	}
	switch typed := composite.(type) {
	case *hujson.Object:
		added.BeforeExtra = colonSpace(typed, inline)
		member := hujson.Value{BeforeExtra: lead, Value: hujson.String(name)}
		typed.Members = append(typed.Members, hujson.ObjectMember{Name: member, Value: added})
	case *hujson.Array:
		added.BeforeExtra = lead
		typed.Elements = append(typed.Elements, added)
	}
	return nil
}

func (d *jsonDocument) openLine(composite hujson.ValueTrimmed, depth int, indent string) hujson.Extra {
	closing := closingExtra(composite)
	closingIndent, ownLine := lineIndent(*closing)
	empty := childCount(composite) == 0 && !hasComment(*closing)
	if !ownLine && !empty {
		return hujson.Extra(d.newline + indent)
	}
	if !ownLine {
		closingIndent = strings.Repeat(d.unit, depth)
	}
	before := []byte(*closing)
	if !empty {
		before = beforeLastLine(*closing)
	}
	lead := slices.Concat(before, []byte(d.newline+indent))
	*closing = hujson.Extra(d.newline + closingIndent)
	return lead
}

func inlineSeparator(composite hujson.ValueTrimmed) hujson.Extra {
	count := childCount(composite)
	if count == 0 {
		return nil
	}
	if count > 1 {
		if last := *leadAt(composite, count-1); !hasComment(last) {
			return slices.Clone(last)
		}
	}
	if object, ok := composite.(*hujson.Object); ok && len(object.Members[0].Value.BeforeExtra) == 0 {
		return nil
	}
	return hujson.Extra(" ")
}

func colonSpace(object *hujson.Object, inline bool) hujson.Extra {
	if count := len(object.Members); count > 0 {
		if space := object.Members[count-1].Value.BeforeExtra; !hasComment(space) && !bytes.Contains(space, []byte("\n")) {
			return slices.Clone(space)
		}
	}
	if inline {
		return nil
	}
	return hujson.Extra(" ")
}

func removeChild(composite hujson.ValueTrimmed, i int) {
	lead := slices.Clone(*leadAt(composite, i))
	own := slices.Clone(childAt(composite, i).AfterExtra)
	wasLast := i == childCount(composite)-1
	trailingComma := wasLast && childAt(composite, i).AfterExtra != nil
	switch typed := composite.(type) {
	case *hujson.Object:
		typed.Members = slices.Delete(typed.Members, i, i+1)
	case *hujson.Array:
		typed.Elements = slices.Delete(typed.Elements, i, i+1)
	}
	count := childCount(composite)
	next := closingExtra(composite)
	if i < count {
		next = leadAt(composite, i)
	}
	merged := joinAround(lead, *next, i == 0)
	if hasComment(own) {
		merged = slices.Concat(own, merged)
	}
	if count == 0 && !hasComment(merged) {
		merged = emptied(lead, *next)
	}
	*next = merged
	if wasLast && count > 0 {
		keepTrailingComma(composite, trailingComma)
	}
}

func emptied(lead, closing hujson.Extra) hujson.Extra {
	if !bytes.Contains(lead, []byte("\n")) {
		return closing
	}
	return bytes.TrimSuffix(lead[:bytes.LastIndexByte(lead, '\n')], []byte("\r"))
}

func joinAround(before, after hujson.Extra, first bool) hujson.Extra {
	if first && isPlainSpace(before) && isPlainSpace(after) {
		return before
	}
	rest := bytes.TrimLeft(after, " \t")
	if bytes.HasPrefix(rest, []byte("\n")) || bytes.HasPrefix(rest, []byte("\r")) {
		return slices.Concat(beforeLastLine(before), after)
	}
	if !hasComment(before) {
		return after
	}
	return slices.Concat(before, after)
}

func isPlainSpace(extra hujson.Extra) bool {
	return !hasComment(extra) && !bytes.Contains(extra, []byte("\n"))
}

func keepTrailingComma(composite hujson.ValueTrimmed, wanted bool) {
	last := childAt(composite, childCount(composite)-1)
	if wanted {
		if last.AfterExtra == nil {
			last.AfterExtra = hujson.Extra{}
		}
		return
	}
	closing := closingExtra(composite)
	if len(last.AfterExtra) > 0 {
		*closing = slices.Concat(last.AfterExtra, *closing)
	}
	last.AfterExtra = nil
}
