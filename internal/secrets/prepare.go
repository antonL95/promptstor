package secrets

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/antonL95/promptstor/internal/library"
)

type preparation struct {
	envRef     func(name string) string
	resolver   Resolver
	resolve    bool
	secretKeys []string
	needsSetup []string
}

func Prepare(s library.MCPServer, envRef func(name string) string, r Resolver) (out library.MCPServer, secretKeys, needsSetup []string, err error) {
	p := &preparation{envRef: envRef, resolver: r, resolve: s.Resolve}
	env, err := p.section(envSection, s.Env)
	if err != nil {
		return library.MCPServer{}, nil, nil, err
	}
	headers, err := p.section(headersSection, s.Headers)
	if err != nil {
		return library.MCPServer{}, nil, nil, err
	}
	command, err := p.text(commandKey, s.Command)
	if err != nil {
		return library.MCPServer{}, nil, nil, err
	}
	args, err := p.arguments(s.Args)
	if err != nil {
		return library.MCPServer{}, nil, nil, err
	}
	url, err := p.text(urlKey, s.URL)
	if err != nil {
		return library.MCPServer{}, nil, nil, err
	}
	if len(p.needsSetup) > 0 {
		slices.Sort(p.needsSetup)
		return library.MCPServer{}, nil, p.needsSetup, nil
	}
	s.Env, s.Headers, s.Command, s.Args, s.URL = env, headers, command, args, url
	slices.Sort(p.secretKeys)
	return s, p.secretKeys, nil, nil
}

func (p *preparation) section(section string, values map[string]library.Value) (map[string]library.Value, error) {
	prepared := maps.Clone(values)
	for _, name := range slices.Sorted(maps.Keys(values)) {
		value, err := p.value(section+"/"+name, name, values[name])
		if err != nil {
			return nil, err
		}
		prepared[name] = value
	}
	return prepared, nil
}

func (p *preparation) arguments(args []string) ([]string, error) {
	prepared := slices.Clone(args)
	for i, arg := range args {
		text, err := p.text(argsSection+"/"+strconv.Itoa(i), arg)
		if err != nil {
			return nil, err
		}
		prepared[i] = text
	}
	return prepared, nil
}

func (p *preparation) text(key, text string) (string, error) {
	if !canonicalPlaceholder.MatchString(text) {
		return text, nil
	}
	value, err := p.interpolation(key, text)
	return value.Literal, err
}

func (p *preparation) value(key, name string, value library.Value) (library.Value, error) {
	switch {
	case value.Ref != "":
		return p.reference(key, value.Ref)
	case canonicalPlaceholder.MatchString(value.Literal):
		return p.interpolation(key, value.Literal)
	case IsSecretName(name):
		return p.manual(key), nil
	default:
		return value, nil
	}
}

func (p *preparation) reference(key, ref string) (library.Value, error) {
	if name, found := strings.CutPrefix(ref, envScheme); found && validEnvName.MatchString(name) {
		return p.variable(key, name)
	}
	if strings.HasPrefix(ref, keychainScheme) && p.resolve {
		return p.resolved(key, ref)
	}
	return p.manual(key), nil
}

func (p *preparation) interpolation(key, literal string) (library.Value, error) {
	var failure error
	rewritten := canonicalPlaceholder.ReplaceAllStringFunc(literal, func(placeholder string) string {
		if failure != nil {
			return ""
		}
		value, err := p.variable(key, canonicalPlaceholder.FindStringSubmatch(placeholder)[1])
		failure = err
		return value.Literal
	})
	return library.Value{Literal: rewritten}, failure
}

func (p *preparation) variable(key, name string) (library.Value, error) {
	if p.envRef != nil {
		return library.Value{Literal: p.envRef(name)}, nil
	}
	if p.resolve {
		return p.resolved(key, envScheme+name)
	}
	return p.manual(key), nil
}

func (p *preparation) resolved(key, ref string) (library.Value, error) {
	value, err := p.resolver.Resolve(ref)
	if err != nil {
		return library.Value{}, fmt.Errorf("resolve %s from %s: %w", key, ref, err)
	}
	if !slices.Contains(p.secretKeys, key) {
		p.secretKeys = append(p.secretKeys, key)
	}
	return library.Value{Literal: value}, nil
}

func (p *preparation) manual(key string) library.Value {
	if !slices.Contains(p.needsSetup, key) {
		p.needsSetup = append(p.needsSetup, key)
	}
	return library.Value{}
}
