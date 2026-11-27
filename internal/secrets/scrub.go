package secrets

import (
	"maps"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/library"
)

type scrubber struct {
	notes []string
}

func Scrub(s library.MCPServer) (library.MCPServer, []string) {
	scrub := &scrubber{}
	s.Env = scrub.section(envSection, s.Env)
	s.Headers = scrub.section(headersSection, s.Headers)
	s.Command = scrub.text(s.Command, "COMMAND")
	s.Args = scrub.arguments(s.Args)
	s.URL = scrub.text(s.URL, "URL")
	slices.Sort(scrub.notes)
	return s, slices.Compact(scrub.notes)
}

func (s *scrubber) section(section string, values map[string]library.Value) map[string]library.Value {
	scrubbed := maps.Clone(values)
	for _, name := range slices.Sorted(maps.Keys(values)) {
		value := values[name]
		if value.Ref != "" {
			continue
		}
		scrubbed[name] = s.value(name, value.Literal)
		if strings.HasPrefix(scrubbed[name].Ref, needsSetupScheme) {
			s.notes = append(s.notes, section+"/"+name)
		}
	}
	return scrubbed
}

func (s *scrubber) value(name, literal string) library.Value {
	text := s.userinfo(normalise(literal), upperSnake(name))
	if whole := wholePlaceholder.FindStringSubmatch(text); whole != nil {
		return library.Value{Ref: envScheme + whole[1]}
	}
	if !canonicalPlaceholder.MatchString(text) && IsSecretName(name) {
		return library.Value{Ref: needsSetupScheme + name}
	}
	return library.Value{Literal: text}
}
