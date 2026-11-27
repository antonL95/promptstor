package geminicli

import (
	"fmt"
	"strings"
)

func tomlString(value string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteRune('\\')
			out.WriteRune(r)
		case '\n':
			out.WriteString(`\n`)
		case '\t':
			out.WriteString(`\t`)
		default:
			writeTOMLRune(&out, r)
		}
	}
	out.WriteByte('"')
	return out.String()
}

func tomlMultiline(value string) string {
	var out strings.Builder
	out.WriteString("\"\"\"\n")
	quotes := 0
	for i, r := range value {
		if r != '"' {
			quotes = 0
		}
		switch r {
		case '"':
			if quotes++; quotes == 3 || i == len(value)-1 {
				out.WriteRune('\\')
				quotes = 0
			}
			out.WriteRune(r)
		case '\\':
			out.WriteString(`\\`)
		case '\n', '\t':
			out.WriteRune(r)
		default:
			writeTOMLRune(&out, r)
		}
	}
	out.WriteString(`"""`)
	return out.String()
}

func writeTOMLRune(out *strings.Builder, r rune) {
	if r < 0x20 || r == 0x7f {
		fmt.Fprintf(out, `\u%04X`, r)
		return
	}
	out.WriteRune(r)
}

func tomlArray(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = tomlString(value)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
