package secrets

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	shellVariable     = regexp.MustCompile(`\$\{(` + envName + `)\}|\$(` + envName + `)`)
	shellSubstitution = regexp.MustCompile("\\$\\([^)]*\\)|`[^`]*`")
	headerWord        = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9_-]*)\s*:\s*(.+)$`)
	assignmentWord    = regexp.MustCompile(`^(` + envName + `)=(.*)$`)
	bearerToken       = regexp.MustCompile(`(?i)\bbearer\s+([^\s'"]+)`)
)

const substitutionName = "SUBSTITUTION"

func HoldsLiteral(native any) bool {
	switch value := native.(type) {
	case string:
		return commandHoldsLiteral(value)
	case []any:
		for _, element := range value {
			if HoldsLiteral(element) {
				return true
			}
		}
	case []string:
		for _, element := range value {
			if commandHoldsLiteral(element) {
				return true
			}
		}
	case map[string]any:
		for key, field := range value {
			if text, isText := field.(string); isText && IsSecretName(key) && isLiteral(text) {
				return true
			}
			if HoldsLiteral(field) {
				return true
			}
		}
	}
	return false
}

func commandHoldsLiteral(command string) bool {
	expanded := expandShell(command)
	for _, match := range bearerToken.FindAllStringSubmatch(expanded, -1) {
		if isLiteral(match[1]) {
			return true
		}
	}
	words := shellWords(expanded)
	for _, word := range words {
		if headerHoldsLiteral(word) || assignmentHoldsLiteral(word) {
			return true
		}
	}
	scrub := &scrubber{}
	scrub.arguments(words)
	return len(scrub.notes) > 0
}

func headerHoldsLiteral(word string) bool {
	header := headerWord.FindStringSubmatch(word)
	return header != nil && IsSecretName(header[1]) && isLiteral(header[2])
}

func assignmentHoldsLiteral(word string) bool {
	assignment := assignmentWord.FindStringSubmatch(word)
	return assignment != nil && IsSecretName(assignment[1]) && isLiteral(assignment[2])
}

func isLiteral(value string) bool {
	expanded := strings.TrimSpace(normalise(expandShell(value)))
	return expanded != "" && !canonicalPlaceholder.MatchString(expanded)
}

func expandShell(text string) string {
	text = shellSubstitution.ReplaceAllString(text, placeholder(substitutionName))
	return shellVariable.ReplaceAllStringFunc(text, func(found string) string {
		match := shellVariable.FindStringSubmatch(found)
		if match[1] != "" {
			return placeholder(match[1])
		}
		return placeholder(match[2])
	})
}

func shellWords(text string) []string {
	var words []string
	var word strings.Builder
	inWord, escaped := false, false
	var quote rune
	for _, r := range text {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inWord = true, true
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			word.WriteRune(r)
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case unicode.IsSpace(r):
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
}
