package secrets

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/paths"
)

const literalSecret = "example-literal-value"

type fakeResolver map[string]string

func (f fakeResolver) Resolve(ref string) (string, error) {
	value, found := f[ref]
	if !found {
		return "", errors.New("not found")
	}
	return value, nil
}

func cursorRef(name string) string { return "${env:" + name + "}" }

func literal(s string) library.Value { return library.Value{Literal: s} }

func reference(s string) library.Value { return library.Value{Ref: s} }

func TestIsSecretName(t *testing.T) {
	for _, name := range []string{"API_KEY", "GithubToken", "client_secret", "DB_PASSWORD", "passwd", "Authorization", "bearer", "Cookie", "CREDENTIALS", "SESSION_ID"} {
		if !IsSecretName(name) {
			t.Errorf("%s is not secret", name)
		}
	}
	for _, name := range []string{"LOG_LEVEL", "Accept", "PORT"} {
		if IsSecretName(name) {
			t.Errorf("%s is secret", name)
		}
	}
}

func TestPrepareRefusesLiteralSecret(t *testing.T) {
	server := library.MCPServer{
		Resolve: true,
		Env:     map[string]library.Value{"API_KEY": literal(literalSecret), "LOG_LEVEL": literal("debug")},
		Headers: map[string]library.Value{"Authorization": reference("needs-setup:Authorization")},
	}
	out, secretKeys, needsSetup, err := Prepare(server, cursorRef, fakeResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"env/API_KEY", "headers/Authorization"}; !slices.Equal(needsSetup, want) {
		t.Errorf("needsSetup = %v", needsSetup)
	}
	if !reflect.DeepEqual(out, library.MCPServer{}) || secretKeys != nil {
		t.Errorf("server was written, secretKeys = %v", secretKeys)
	}
}

func TestPrepareUsesNativeEnvRef(t *testing.T) {
	server := library.MCPServer{
		Transport: "stdio",
		Command:   "example-server",
		Resolve:   true,
		Env:       map[string]library.Value{"API_KEY": reference("env:API_KEY"), "LOG_LEVEL": literal("debug")},
		Headers:   map[string]library.Value{"Authorization": literal("Bearer ${env:TOKEN}")},
	}
	out, secretKeys, needsSetup, err := Prepare(server, cursorRef, fakeResolver{})
	if err != nil {
		t.Fatal(err)
	}
	want := server
	want.Env = map[string]library.Value{"API_KEY": literal("${env:API_KEY}"), "LOG_LEVEL": literal("debug")}
	want.Headers = map[string]library.Value{"Authorization": literal("Bearer ${env:TOKEN}")}
	if !reflect.DeepEqual(out, want) || len(secretKeys) != 0 || len(needsSetup) != 0 {
		t.Errorf("out = %+v, secretKeys = %v, needsSetup = %v", out, secretKeys, needsSetup)
	}
	if server.Env["API_KEY"] != reference("env:API_KEY") {
		t.Error("input was mutated")
	}

	braced, _, _, err := Prepare(server, func(name string) string { return "{env:" + name + "}" }, fakeResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if got := braced.Headers["Authorization"]; got != literal("Bearer {env:TOKEN}") {
		t.Errorf("header = %+v", got)
	}
}

func TestPrepareResolvesEnvWithoutNativeRefOnlyWhenOptedIn(t *testing.T) {
	server := library.MCPServer{
		Env:     map[string]library.Value{"API_KEY": reference("env:API_KEY")},
		Headers: map[string]library.Value{"X-Trace": literal("id ${env:TRACE} ${env:TRACE}")},
	}
	resolver := fakeResolver{"env:API_KEY": "resolved-one", "env:TRACE": "resolved-two"}

	_, secretKeys, needsSetup, err := Prepare(server, nil, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"env/API_KEY", "headers/X-Trace"}; !slices.Equal(needsSetup, want) || secretKeys != nil {
		t.Errorf("needsSetup = %v, secretKeys = %v", needsSetup, secretKeys)
	}

	server.Resolve = true
	out, secretKeys, needsSetup, err := Prepare(server, nil, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"env/API_KEY", "headers/X-Trace"}; !slices.Equal(secretKeys, want) || needsSetup != nil {
		t.Errorf("secretKeys = %v, needsSetup = %v", secretKeys, needsSetup)
	}
	if out.Env["API_KEY"] != literal("resolved-one") || out.Headers["X-Trace"] != literal("id resolved-two resolved-two") {
		t.Error("values were not resolved")
	}
}

func TestPrepareResolvesKeychainWhenOptedIn(t *testing.T) {
	server := library.MCPServer{Env: map[string]library.Value{"API_KEY": reference("keychain:example/account")}}
	resolver := fakeResolver{"keychain:example/account": "resolved"}

	out, _, needsSetup, err := Prepare(server, cursorRef, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(needsSetup, []string{"env/API_KEY"}) || out.Env != nil {
		t.Errorf("needsSetup = %v", needsSetup)
	}

	server.Resolve = true
	out, secretKeys, needsSetup, err := Prepare(server, cursorRef, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if out.Env["API_KEY"] != literal("resolved") || !slices.Equal(secretKeys, []string{"env/API_KEY"}) || needsSetup != nil {
		t.Errorf("secretKeys = %v, needsSetup = %v", secretKeys, needsSetup)
	}
}

func TestPrepareReportsUnknownOrMalformedRefs(t *testing.T) {
	server := library.MCPServer{
		Resolve: true,
		Env:     map[string]library.Value{"A": reference("vault:example"), "B": reference("env:"), "C": reference("env:not a name")},
	}
	_, _, needsSetup, err := Prepare(server, cursorRef, fakeResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"env/A", "env/B", "env/C"}; !slices.Equal(needsSetup, want) {
		t.Errorf("needsSetup = %v", needsSetup)
	}
}

func TestPrepareResolverFailureNamesKeyAndRef(t *testing.T) {
	server := library.MCPServer{
		Resolve: true,
		Env:     map[string]library.Value{"API_KEY": reference("keychain:example/account"), "OTHER": literal(literalSecret)},
	}
	out, secretKeys, needsSetup, err := Prepare(server, nil, fakeResolver{})
	if err == nil {
		t.Fatal("no error")
	}
	message := err.Error()
	if !strings.Contains(message, "env/API_KEY") || !strings.Contains(message, "keychain:example/account") {
		t.Errorf("error = %s", message)
	}
	if strings.Contains(message, literalSecret) {
		t.Error("error carries a value")
	}
	if !reflect.DeepEqual(out, library.MCPServer{}) || secretKeys != nil || needsSetup != nil {
		t.Error("partial result returned")
	}
}

func TestKeychainCommandLines(t *testing.T) {
	for goos, want := range map[string][]string{
		"darwin": {"security", "find-generic-password", "-s", "example/service", "-a", "account", "-w"},
		"linux":  {"secret-tool", "lookup", "service", "example/service", "account", "account"},
	} {
		var got []string
		resolver := OSResolver(paths.Env{OS: goos}, func(name string, args ...string) ([]byte, error) {
			got = append([]string{name}, args...)
			return []byte("resolved\n\n"), nil
		})
		value, err := resolver.Resolve("keychain:example/service/account")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want) || value != "resolved\n" {
			t.Errorf("%s: ran %v, value %q", goos, got, value)
		}
	}
}

func TestOSResolverReadsEnvAndReportsFailures(t *testing.T) {
	ran := 0
	run := func(string, ...string) ([]byte, error) {
		ran++
		return []byte(literalSecret), errors.New("exit status 44")
	}
	env := paths.Env{OS: "darwin", Getenv: func(name string) string {
		return map[string]string{"SET": "resolved"}[name]
	}}
	resolver := OSResolver(env, run)
	if value, err := resolver.Resolve("env:SET"); err != nil || value != "resolved" {
		t.Errorf("env:SET = %q, %v", value, err)
	}
	for _, ref := range []string{"env:UNSET", "keychain:no-account", "keychain:/account", "keychain:service/", "needs-setup:X"} {
		if _, err := resolver.Resolve(ref); err == nil {
			t.Errorf("%s resolved", ref)
		}
	}
	if ran != 0 {
		t.Errorf("ran %d commands", ran)
	}
	value, err := resolver.Resolve("keychain:service/account")
	if err == nil || value != "" || strings.Contains(err.Error(), literalSecret) {
		t.Error("a failed command must give an error without output")
	}
	other := OSResolver(paths.Env{OS: "windows"}, run)
	if _, err := other.Resolve("keychain:service/account"); err == nil || ran != 1 {
		t.Errorf("windows: err = %v, ran = %d", err, ran)
	}
	if _, err := OSResolver(paths.Env{}, run).Resolve("env:SET"); err == nil {
		t.Error("env without Getenv resolved")
	}
}

func TestScrubRemovesLiteralsAndNormalisesInterpolation(t *testing.T) {
	server := library.MCPServer{
		Env: map[string]library.Value{
			"API_KEY":      literal(literalSecret),
			"BRACED":       literal("${ONE}"),
			"DEFAULTED":    literal("${TWO:-" + literalSecret + "}"),
			"NESTED":       literal("${TWO:-${THREE}}"),
			"BARE":         literal("$FOUR"),
			"OPENCODE":     literal("{env:FIVE}"),
			"CURSOR_TOKEN": literal("${env:SIX}"),
			"PASSWORD":     literal("abc$def"),
			"DB_SECRET":    literal("abc $9"),
			"LOG_LEVEL":    literal("debug"),
			"PRICE":        literal("cost$USD"),
			"PATHS":        literal("${ONE}:/opt:{env:FIVE}"),
			"KEPT_TOKEN":   reference("keychain:example/account"),
		},
		Headers: map[string]library.Value{
			"Authorization": literal("Bearer ${TOKEN}"),
			"X-Api-Key":     literal("Token $KEY now"),
			"Cookie":        literal(literalSecret),
		},
	}
	out, needsSetup := Scrub(server)
	wantEnv := map[string]library.Value{
		"API_KEY":      reference("needs-setup:API_KEY"),
		"BRACED":       reference("env:ONE"),
		"DEFAULTED":    reference("env:TWO"),
		"NESTED":       reference("env:TWO"),
		"BARE":         reference("env:FOUR"),
		"OPENCODE":     reference("env:FIVE"),
		"CURSOR_TOKEN": reference("env:SIX"),
		"PASSWORD":     reference("needs-setup:PASSWORD"),
		"DB_SECRET":    reference("needs-setup:DB_SECRET"),
		"LOG_LEVEL":    literal("debug"),
		"PRICE":        literal("cost$USD"),
		"PATHS":        literal("${env:ONE}:/opt:${env:FIVE}"),
		"KEPT_TOKEN":   reference("keychain:example/account"),
	}
	wantHeaders := map[string]library.Value{
		"Authorization": literal("Bearer ${env:TOKEN}"),
		"X-Api-Key":     literal("Token ${env:KEY} now"),
		"Cookie":        reference("needs-setup:Cookie"),
	}
	for name, want := range wantEnv {
		if out.Env[name] != want {
			t.Errorf("env %s was scrubbed wrongly", name)
		}
	}
	for name, want := range wantHeaders {
		if out.Headers[name] != want {
			t.Errorf("header %s was scrubbed wrongly", name)
		}
	}
	if len(out.Env) != len(wantEnv) || len(out.Headers) != len(wantHeaders) {
		t.Errorf("sizes = %d, %d", len(out.Env), len(out.Headers))
	}
	want := []string{"env/API_KEY", "env/DB_SECRET", "env/PASSWORD", "headers/Cookie"}
	if !slices.Equal(needsSetup, want) {
		t.Errorf("needsSetup = %v", needsSetup)
	}
	if server.Env["BRACED"] != literal("${ONE}") || server.Headers["Cookie"].Ref != "" {
		t.Error("input was mutated")
	}
}
