package secrets

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
)

const (
	envScheme        = "env:"
	keychainScheme   = "keychain:"
	needsSetupScheme = "needs-setup:"

	envName = `[A-Za-z_][A-Za-z0-9_]*`

	envSection     = "env"
	headersSection = "headers"
)

var (
	secretName           = regexp.MustCompile(`(?i)(key|token|secret|password|passwd|auth|bearer|cookie|credential|session)`)
	validEnvName         = regexp.MustCompile(`^` + envName + `$`)
	canonicalPlaceholder = regexp.MustCompile(`\$\{env:(` + envName + `)\}`)
	nativePlaceholder    = regexp.MustCompile(
		`\$\{env:(` + envName + `)\}` +
			`|\$\{(` + envName + `)(?::-(?:[^{}]|\{[^{}]*\})*)?\}` +
			`|\{env:(` + envName + `)\}` +
			`|\$(` + envName + `)`)
)

const bareGroup = 4

func IsSecretName(name string) bool { return secretName.MatchString(name) }

type Resolver interface {
	Resolve(ref string) (string, error)
}

type osResolver struct {
	env paths.Env
	run func(name string, args ...string) ([]byte, error)
}

func OSResolver(env paths.Env, run func(name string, args ...string) ([]byte, error)) Resolver {
	return osResolver{env: env, run: run}
}

func (r osResolver) Resolve(ref string) (string, error) {
	if name, found := strings.CutPrefix(ref, envScheme); found {
		return r.variable(name)
	}
	if entry, found := strings.CutPrefix(ref, keychainScheme); found {
		return r.keychain(entry)
	}
	return "", errors.New("unknown reference scheme")
}

func (r osResolver) variable(name string) (string, error) {
	if r.env.Getenv != nil {
		if value := r.env.Getenv(name); value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("environment variable %s is unset or empty", name)
}

func (r osResolver) keychain(entry string) (string, error) {
	split := strings.LastIndex(entry, "/")
	if split <= 0 || split == len(entry)-1 {
		return "", errors.New("a keychain reference is keychain:<service>/<account>")
	}
	service, account := entry[:split], entry[split+1:]
	var command string
	var args []string
	switch r.env.OS {
	case "darwin":
		command, args = "security", []string{"find-generic-password", "-s", service, "-a", account, "-w"}
	case "linux":
		command, args = "secret-tool", []string{"lookup", "service", service, "account", account}
	default:
		return "", fmt.Errorf("keychain references are not supported on %q", r.env.OS)
	}
	output, err := r.run(command, args...)
	if err != nil {
		return "", fmt.Errorf("run %s: %w", command, err)
	}
	return strings.TrimSuffix(string(output), "\n"), nil
}

func Scrub(s library.MCPServer) (library.MCPServer, []string) {
	var needsSetup []string
	scrub := func(section string, values map[string]library.Value) map[string]library.Value {
		scrubbed := maps.Clone(values)
		for _, name := range slices.Sorted(maps.Keys(values)) {
			value := values[name]
			if value.Ref != "" {
				continue
			}
			scrubbed[name] = scrubValue(name, value.Literal)
			if strings.HasPrefix(scrubbed[name].Ref, needsSetupScheme) {
				needsSetup = append(needsSetup, section+"/"+name)
			}
		}
		return scrubbed
	}
	s.Env = scrub(envSection, s.Env)
	s.Headers = scrub(headersSection, s.Headers)
	return s, needsSetup
}

func scrubValue(name, literal string) library.Value {
	var rewritten strings.Builder
	copied, found := 0, false
	for _, match := range nativePlaceholder.FindAllStringSubmatchIndex(literal, -1) {
		start, end := match[0], match[1]
		variable, bare := capturedName(literal, match)
		if bare && !isDelimited(literal, start, end) {
			continue
		}
		if start == 0 && end == len(literal) {
			return library.Value{Ref: envScheme + variable}
		}
		rewritten.WriteString(literal[copied:start])
		rewritten.WriteString("${" + envScheme + variable + "}")
		copied, found = end, true
	}
	switch {
	case found:
		rewritten.WriteString(literal[copied:])
		return library.Value{Literal: rewritten.String()}
	case IsSecretName(name):
		return library.Value{Ref: needsSetupScheme + name}
	default:
		return library.Value{Literal: literal}
	}
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
	if len(p.needsSetup) > 0 {
		slices.Sort(p.needsSetup)
		return library.MCPServer{}, nil, p.needsSetup, nil
	}
	s.Env, s.Headers = env, headers
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
