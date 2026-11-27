package agentsmd

import (
	"regexp"
	"strings"

	"github.com/antonL95/promptstor/internal/library"
)

const (
	scopePrefix = "Applies to: "
	agentPrefix = "Applies when relevant: "
)

var (
	headingLine    = regexp.MustCompile(`^#{1,6} `)
	annotationLine = regexp.MustCompile("^(" + scopePrefix + "`[^`]*`(, `[^`]*`)*|" + agentPrefix + ".*)$")
)

func annotated(it library.Item) library.Item {
	note := annotation(it)
	if note == "" {
		return it
	}
	rest := trimBlankStart(it.Body)
	var heading string
	if headingLine.MatchString(rest) {
		first, after, _ := strings.Cut(rest, "\n")
		heading, rest = first+"\n", trimBlankStart(after)
	}
	if first, after, _ := strings.Cut(rest, "\n"); annotationLine.MatchString(first) {
		rest = trimBlankStart(after)
	}
	it.Body = heading + note + "\n"
	if rest != "" {
		it.Body += "\n" + rest
	}
	return it
}

func annotation(it library.Item) string {
	switch activationOf(it) {
	case library.ActivationFiles:
		if len(it.Scope) == 0 {
			return ""
		}
		return scopePrefix + "`" + strings.Join(it.Scope, "`, `") + "`"
	case library.ActivationAgent:
		description := strings.Join(strings.Fields(it.Description), " ")
		if description == "" {
			return ""
		}
		return agentPrefix + description
	default:
		return ""
	}
}

func trimBlankStart(text string) string {
	for {
		first, after, found := strings.Cut(text, "\n")
		if strings.TrimSpace(first) != "" {
			return text
		}
		if !found {
			return ""
		}
		text = after
	}
}
