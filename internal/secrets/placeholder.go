package secrets

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	nativePlaceholder = regexp.MustCompile(
		`\$\{env:(` + envName + `)\}` +
			`|\$\{(` + envName + `)(?::-(?:[^{}]|\{[^{}]*\})*)?\}` +
			`|\{env:(` + envName + `)\}` +
			`|\$(` + envName + `)`)
	wholePlaceholder = regexp.MustCompile(`^` + canonicalPlaceholder.String() + `$`)
)

const bareGroup = 4

func placeholder(name string) string { return "${" + envScheme + name + "}" }

func normalise(text string) string {
	var rewritten strings.Builder
	copied := 0
	for _, match := range nativePlaceholder.FindAllStringSubmatchIndex(text, -1) {
		start, end := match[0], match[1]
		name, bare := capturedName(text, match)
		if bare && !isDelimited(text, start, end) {
			continue
		}
		rewritten.WriteString(text[copied:start])
		rewritten.WriteString(placeholder(name))
		copied = end
	}
	rewritten.WriteString(text[copied:])
	return rewritten.String()
}

func mask(text string) string {
	return canonicalPlaceholder.ReplaceAllStringFunc(text, func(found string) string {
		return strings.Repeat("x", len(found))
	})
}

func capturedName(literal string, match []int) (name string, bare bool) {
	for group := 1; group <= bareGroup; group++ {
		if start := match[2*group]; start >= 0 {
			return literal[start:match[2*group+1]], group == bareGroup
		}
	}
	return "", false
}

func isDelimited(s string, start, end int) bool {
	before, _ := utf8.DecodeLastRuneInString(s[:start])
	after, _ := utf8.DecodeRuneInString(s[end:])
	return (start == 0 || unicode.IsSpace(before)) && (end == len(s) || unicode.IsSpace(after))
}
