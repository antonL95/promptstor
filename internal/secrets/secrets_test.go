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

func checkStrings(t *testing.T, field string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s has %d entries, want %d", field, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s %d was handled wrongly, want %q", field, i, want[i])
		}
	}
}

func TestScrubArgsSecretFlag(t *testing.T) {
	server := library.MCPServer{
		Command: "$BIN",
		Args: []string{
			"--api-key=" + literalSecret,
			"--authToken", literalSecret,
			"--client-secret", "-" + literalSecret,
			"--password=$PW",
			"--session", "${SESSION:-" + literalSecret + "}",
			"--token=",
			"--no-auth", "--verbose",
			"--use-cookie", "-v",
			"--port=8080",
			"--root", "{env:ROOT}",
			"--home=$HOME",
			"--APIKey", literalSecret,
			"awk $1",
			"--secret",
		},
	}
	original := slices.Clone(server.Args)
	out, notes := Scrub(server)
	checkStrings(t, "args", out.Args, []string{
		"--api-key=${env:API_KEY}",
		"--authToken", "${env:AUTH_TOKEN}",
		"--client-secret", "${env:CLIENT_SECRET}",
		"--password=${env:PW}",
		"--session", "${env:SESSION}",
		"--token=",
		"--no-auth", "--verbose",
		"--use-cookie", "-v",
		"--port=8080",
		"--root", "${env:ROOT}",
		"--home=${env:HOME}",
		"--APIKey", "${env:API_KEY}",
		"awk $1",
		"--secret",
	})
	if out.Command != "${env:BIN}" {
		t.Error("command was not normalised")
	}
	if want := []string{"set API_KEY", "set AUTH_TOKEN", "set CLIENT_SECRET"}; !slices.Equal(notes, want) {
		t.Errorf("notes = %v", notes)
	}
	if !slices.Equal(server.Args, original) {
		t.Error("input was mutated")
	}
}

func TestScrubURLQueryAndUserinfo(t *testing.T) {
	server := library.MCPServer{
		URL: "https://user:" + literalSecret + "@example.invalid/mcp?team=a&api_key=" + literalSecret + "&access%5Ftoken=" + literalSecret + "&auth=$AUTH&key=#part",
		Args: []string{
			"postgres://user:p@" + literalSecret + "@db.invalid:5432/app",
			"--database-url=postgres://user:" + literalSecret + "@db.invalid/app",
			"--redis-url", "redis://:" + literalSecret + "@cache.invalid",
			"https://example.invalid/?token=" + literalSecret,
			"https://${USER}:$PW@example.invalid",
			"https://user@example.invalid:8080/a:b@c",
		},
		Env: map[string]library.Value{
			"DATABASE_URL": literal("postgres://user:" + literalSecret + "@db.invalid/app"),
			"SESSION_URL":  literal("redis://:" + literalSecret + "@cache.invalid"),
			"PROXY":        literal("http://{env:PROXY_USER}:${PROXY_PASS:-" + literalSecret + "}@proxy.invalid"),
			"PLAIN_URL":    literal("https://example.invalid/a"),
		},
		Headers: map[string]library.Value{"X-Upstream": literal("https://user:" + literalSecret + "@example.invalid")},
	}
	out, notes := Scrub(server)
	if out.URL != "https://user:${env:URL_PASSWORD}@example.invalid/mcp?team=a&api_key=${env:API_KEY}&access%5Ftoken=${env:ACCESS_TOKEN}&auth=${env:AUTH}&key=#part" {
		t.Error("url was scrubbed wrongly")
	}
	checkStrings(t, "args", out.Args, []string{
		"postgres://user:${env:ARG_PASSWORD}@db.invalid:5432/app",
		"--database-url=postgres://user:${env:DATABASE_URL_PASSWORD}@db.invalid/app",
		"--redis-url", "redis://:${env:REDIS_URL_PASSWORD}@cache.invalid",
		"https://example.invalid/?token=${env:TOKEN}",
		"https://${env:USER}:${env:PW}@example.invalid",
		"https://user@example.invalid:8080/a:b@c",
	})
	for name, want := range map[string]string{
		"DATABASE_URL": "postgres://user:${env:DATABASE_URL_PASSWORD}@db.invalid/app",
		"SESSION_URL":  "redis://:${env:SESSION_URL_PASSWORD}@cache.invalid",
		"PROXY":        "http://${env:PROXY_USER}:${env:PROXY_PASS}@proxy.invalid",
		"PLAIN_URL":    "https://example.invalid/a",
	} {
		if out.Env[name] != literal(want) {
			t.Errorf("env %s was scrubbed wrongly", name)
		}
	}
	if out.Headers["X-Upstream"] != literal("https://user:${env:X_UPSTREAM_PASSWORD}@example.invalid") {
		t.Error("header was scrubbed wrongly")
	}
	want := []string{
		"set ACCESS_TOKEN", "set API_KEY", "set ARG_PASSWORD", "set DATABASE_URL_PASSWORD", "set REDIS_URL_PASSWORD",
		"set SESSION_URL_PASSWORD", "set TOKEN", "set URL_PASSWORD", "set X_UPSTREAM_PASSWORD",
	}
	if !slices.Equal(notes, want) {
		t.Errorf("notes = %v", notes)
	}
}

func TestPrepareRendersPlaceholdersInArgsAndURL(t *testing.T) {
	server := library.MCPServer{
		Command: "${env:BIN}",
		URL:     "https://example.invalid/?key=${env:API_KEY}",
		Args:    []string{"--api-key=${env:API_KEY}", "plain"},
	}
	original := slices.Clone(server.Args)

	out, secretKeys, needsSetup, err := Prepare(server, cursorRef, fakeResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, server) || secretKeys != nil || needsSetup != nil {
		t.Errorf("out = %+v, secretKeys = %v, needsSetup = %v", out, secretKeys, needsSetup)
	}
	braced, _, _, err := Prepare(server, func(name string) string { return "{env:" + name + "}" }, fakeResolver{})
	if err != nil {
		t.Fatal(err)
	}
	checkStrings(t, "args", braced.Args, []string{"--api-key={env:API_KEY}", "plain"})
	if braced.Command != "{env:BIN}" || braced.URL != "https://example.invalid/?key={env:API_KEY}" {
		t.Errorf("command = %q, url = %q", braced.Command, braced.URL)
	}

	keys := []string{"args/0", "command", "url"}
	out, _, needsSetup, err = Prepare(server, nil, fakeResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(needsSetup, keys) || !reflect.DeepEqual(out, library.MCPServer{}) {
		t.Errorf("needsSetup = %v", needsSetup)
	}

	server.Resolve = true
	out, secretKeys, needsSetup, err = Prepare(server, nil, fakeResolver{"env:BIN": "bin", "env:API_KEY": "resolved"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(secretKeys, keys) || needsSetup != nil {
		t.Errorf("secretKeys = %v, needsSetup = %v", secretKeys, needsSetup)
	}
	checkStrings(t, "args", out.Args, []string{"--api-key=resolved", "plain"})
	if out.Command != "bin" || out.URL != "https://example.invalid/?key=resolved" {
		t.Error("values were not resolved")
	}
	if !slices.Equal(server.Args, original) {
		t.Error("input was mutated")
	}

	_, _, _, err = Prepare(server, nil, fakeResolver{"env:BIN": "bin"})
	if err == nil || !strings.Contains(err.Error(), "args/0") || !strings.Contains(err.Error(), "env:API_KEY") {
		t.Errorf("error = %v", err)
	}
}
