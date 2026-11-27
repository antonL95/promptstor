package secrets

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
)

var (
	flagWithValue = regexp.MustCompile(`(?s)^--([^=\s]+)=(.*)$`)
	longFlag      = regexp.MustCompile(`^--([^=\s]+)$`)
	shortFlag     = regexp.MustCompile(`^-[A-Za-z]$`)
	urlScheme     = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://`)
	authorityEnd  = regexp.MustCompile(`[/?#\s]`)
	camelWord     = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	acronymWord   = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	nonEnvName    = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

func (s *scrubber) arguments(args []string) []string {
	scrubbed := slices.Clone(args)
	label := "ARG"
	for i := 0; i < len(args); i++ {
		if pair := flagWithValue.FindStringSubmatch(args[i]); pair != nil {
			scrubbed[i] = "--" + pair[1] + "=" + s.flagValue(pair[1], pair[2])
			label = "ARG"
			continue
		}
		flag := longFlag.FindStringSubmatch(args[i])
		if flag == nil {
			scrubbed[i] = s.text(args[i], label)
			label = "ARG"
			continue
		}
		label = upperSnake(flag[1])
		if IsSecretName(flag[1]) && i+1 < len(args) && !isFlag(args[i+1]) {
			i++
			scrubbed[i] = s.secret(args[i], flag[1])
			label = "ARG"
		}
	}
	return scrubbed
}

func isFlag(arg string) bool {
	return strings.HasPrefix(arg, "--") || shortFlag.MatchString(arg)
}

func (s *scrubber) flagValue(flag, value string) string {
	if IsSecretName(flag) {
		return s.secret(value, flag)
	}
	return s.text(value, upperSnake(flag))
}

func (s *scrubber) secret(value, name string) string {
	normalised := normalise(value)
	if normalised == "" || canonicalPlaceholder.MatchString(normalised) {
		return normalised
	}
	variable := upperSnake(name)
	s.notes = append(s.notes, "set "+variable)
	return placeholder(variable)
}

func (s *scrubber) text(text, label string) string {
	return s.query(s.userinfo(normalise(text), label))
}

func (s *scrubber) userinfo(text, label string) string {
	masked := mask(text)
	var rewritten strings.Builder
	copied := 0
	for _, scheme := range urlScheme.FindAllStringIndex(masked, -1) {
		start := scheme[1]
		authority := masked[start:]
		if end := authorityEnd.FindStringIndex(authority); end != nil {
			authority = authority[:end[0]]
		}
		at := strings.LastIndex(authority, "@")
		if start < copied || at < 0 {
			continue
		}
		colon := strings.Index(authority[:at], ":")
		if colon < 0 {
			continue
		}
		rewritten.WriteString(text[copied:start])
		rewritten.WriteString(normalise(text[start : start+colon]))
		rewritten.WriteString(":")
		rewritten.WriteString(s.secret(text[start+colon+1:start+at], label+"_PASSWORD"))
		copied = start + at
	}
	rewritten.WriteString(text[copied:])
	return rewritten.String()
}

func (s *scrubber) query(text string) string {
	masked := mask(text)
	if scheme := urlScheme.FindStringIndex(masked); scheme == nil || scheme[0] != 0 {
		return text
	}
	end := strings.Index(masked, "#")
	if end < 0 {
		end = len(masked)
	}
	start := strings.Index(masked[:end], "?")
	if start < 0 {
		return text
	}
	parameters := strings.Split(text[start+1:end], "&")
	for i, parameter := range parameters {
		key, value, found := strings.Cut(parameter, "=")
		name, err := url.QueryUnescape(key)
		if err != nil {
			name = key
		}
		if found && IsSecretName(name) {
			parameters[i] = key + "=" + s.secret(value, name)
		}
	}
	return text[:start+1] + strings.Join(parameters, "&") + text[end:]
}

func upperSnake(name string) string {
	words := acronymWord.ReplaceAllString(camelWord.ReplaceAllString(name, "${1}_${2}"), "${1}_${2}")
	joined := strings.ToUpper(strings.Trim(nonEnvName.ReplaceAllString(words, "_"), "_"))
	if !validEnvName.MatchString(joined) {
		return "_" + joined
	}
	return joined
}
