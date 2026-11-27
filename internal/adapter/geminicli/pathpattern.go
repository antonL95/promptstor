package geminicli

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxPatternLength  = 1500
	argumentStart     = `\x00"file_path":"`
	argumentEnd       = `"\x00`
	anyDirectories    = `(?:|[^\x00]*/)`
	anyRemainder      = `[^\x00]*`
	withinSegment     = `[^/\x00]*`
	oneCharacter      = `[^/\x00]`
	anyDepth          = "**"
	homePrefix        = "~/"
	unsupportedInGlob = `{}[]"\`
)

func argsPattern(glob, home string) (string, bool) {
	if !isTranslatable(glob) {
		return "", false
	}
	var start, rest string
	if remainder, found := strings.CutPrefix(glob, homePrefix); found {
		start, rest = "(?:~|"+regexp.QuoteMeta(home)+")/", remainder
	} else if remainder, found := strings.CutPrefix(glob, "/"); found {
		start, rest = "/", remainder
	} else {
		start, rest = anyDirectories, strings.TrimPrefix(glob, anyDepth+"/")
	}
	segments := strings.Split(rest, "/")
	if slices.ContainsFunc(segments, func(segment string) bool { return segment == "" || segment == "." || segment == ".." }) {
		return "", false
	}
	var body strings.Builder
	for i, segment := range segments {
		last := i == len(segments)-1
		switch {
		case segment == anyDepth && last:
			body.WriteString(anyRemainder)
		case segment == anyDepth:
			body.WriteString(anyDirectories)
		default:
			writeSegment(&body, segment)
			if !last {
				body.WriteString("/")
			}
		}
	}
	pattern := argumentStart + start + body.String() + argumentEnd
	return pattern, len(pattern) <= maxPatternLength
}

func isTranslatable(glob string) bool {
	switch {
	case !utf8.ValidString(glob) || strings.HasPrefix(glob, "!"):
		return false
	case strings.ContainsAny(glob, unsupportedInGlob) || strings.ContainsFunc(glob, unicode.IsControl):
		return false
	default:
		return !strings.HasPrefix(glob, "~") || strings.HasPrefix(glob, homePrefix)
	}
}

func writeSegment(body *strings.Builder, segment string) {
	for _, r := range segment {
		switch r {
		case '*':
			body.WriteString(withinSegment)
		case '?':
			body.WriteString(oneCharacter)
		default:
			body.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
}
