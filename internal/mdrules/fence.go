package mdrules

import "strings"

const (
	minFenceLength = 3
	maxFenceIndent = 3
)

type Fence struct {
	marker byte
	length int
}

func (f Fence) IsOpen() bool { return f.length > 0 }

func (f Fence) After(line string) Fence {
	marker, length, info, ok := fenceLine(line)
	switch {
	case !ok:
		return f
	case !f.IsOpen():
		if marker == '`' && strings.ContainsRune(info, '`') {
			return f
		}
		return Fence{marker: marker, length: length}
	case marker == f.marker && length >= f.length && strings.TrimSpace(info) == "":
		return Fence{}
	default:
		return f
	}
}

func fenceLine(line string) (marker byte, length int, info string, ok bool) {
	text := strings.TrimLeft(line, " ")
	if len(line)-len(text) > maxFenceIndent || text == "" || (text[0] != '`' && text[0] != '~') {
		return 0, 0, "", false
	}
	marker = text[0]
	info = strings.TrimLeft(text, string(marker))
	length = len(text) - len(info)
	return marker, length, info, length >= minFenceLength
}
