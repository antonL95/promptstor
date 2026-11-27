package structedit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/pelletier/go-toml/v2"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func jsonSet(t *testing.T, doc []byte, key, value string) []byte {
	t.Helper()
	out, err := JSONSet(doc, key, []byte(value))
	if err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
	return out
}

func jsonDelete(t *testing.T, doc []byte, key string) []byte {
	t.Helper()
	out, err := JSONDelete(doc, key)
	if err != nil {
		t.Fatalf("delete %s: %v", key, err)
	}
	return out
}

func assertBytes(t *testing.T, got []byte, want string) {
	t.Helper()
	if string(got) != want {
		t.Errorf("bytes differ\n--- want\n%q\n--- got\n%q", want, got)
	}
}

func replaceOnce(t *testing.T, doc []byte, old, replacement string) string {
	t.Helper()
	if strings.Count(string(doc), old) != 1 {
		t.Fatalf("%q must occur exactly once in the document", old)
	}
	return strings.Replace(string(doc), old, replacement, 1)
}

func TestJSONSetThenDeleteRestoresBytes(t *testing.T) {
	cases := map[string][]string{
		"settings.json": {
			"/added", "/permissions/ask", "/env/LEVEL", "/hooks/Stop", "/hooks/PreToolUse/0/timeout",
			"/permissions/deny@=Bash(rm:*)", "/permissions/allow@=Bash(pwd)",
		},
		"opencode.jsonc": {
			"/added", "/mcp/search", "/mcp/docs/enabled", "/permission/webfetch", "/instructions@=docs/more.md",
			"/mcp/docs/command@=--verbose",
		},
		"gemini-settings.json": {
			"/added", "/general/theme", "/mcpServers/search", "/mcpServers/docs/cwd", "/context/fileName@=MORE.md",
		},
	}
	for name, keys := range cases {
		for _, key := range keys {
			t.Run(name+" "+key, func(t *testing.T) {
				doc := fixture(t, name)
				value := `{"a": [1, 2], "b": {}}`
				if _, element, found := strings.Cut(key, "@="); found {
					value = `"` + element + `"`
				}
				set := jsonSet(t, doc, key, value)
				got, found, err := JSONGet(set, key)
				if err != nil || !found {
					t.Fatalf("get after set: found %v, err %v\n%s", found, err, set)
				}
				assertSameJSON(t, got, value)
				assertBytes(t, jsonDelete(t, set, key), string(doc))
			})
		}
	}
}

func assertSameJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	gotHash, err := manifest.Hash(manifest.UnitJSON, got)
	if err != nil {
		t.Fatalf("%v: %s", err, got)
	}
	wantHash, err := manifest.Hash(manifest.UnitJSON, []byte(want))
	if err != nil {
		t.Fatal(err)
	}
	if gotHash != wantHash {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestJSONSetDeleteRestoresLayouts(t *testing.T) {
	docs := []string{
		"{}", "{}\n", "{\n  \"a\": 1\n}", "{\n  \"a\": 1,\n}\n", "{\"a\":1}", "{ \"a\": 1, \"c\": 2 }\n",
		"{\n    \"a\": 1 // note\n}\n", "{\n  \"a\": 1, // note\n}\n", "{\n  // empty for now\n}\n",
		"{\r\n  \"a\": 1\r\n}\r\n", "{\n  \"a\": 1\n\n}\n", "{\n  \"a\": 1 /* b */\n}\n",
		"{\n  \"a\": 1\n  // last\n}\n",
		"{\n  \"a\": 1 }\n", "{\n  \"a\": 1 /* b */}\n", "{\n  \"a\": 1\n  ,}\n", "{\n  \"a\": 1 /* b */ }\n", "{\n  /* b */ }\n",
		"{ }\n", "{\n}\n", "{\n  }", "{\n\n}\n", "{\r\n}\r\n",
	}
	for _, doc := range docs {
		t.Run(doc, func(t *testing.T) {
			set := jsonSet(t, []byte(doc), "/b", `{"c": true}`)
			assertSameJSON(t, mustGet(t, set, "/b"), `{"c": true}`)
			assertBytes(t, jsonDelete(t, set, "/b"), doc)
		})
	}
}

func mustGet(t *testing.T, doc []byte, key string) []byte {
	t.Helper()
	got, found, err := JSONGet(doc, key)
	if err != nil || !found {
		t.Fatalf("get %s: found %v, err %v\n%s", key, found, err, doc)
	}
	return got
}

func TestJSONSetFormatsOnlyNewValues(t *testing.T) {
	cases := []struct{ name, doc, key, value, want string }{
		{"empty document", "", "/a/b", `1`, "{\n  \"a\": {\n    \"b\": 1\n  }\n}\n"},
		{"whitespace document", " \n", "/a", `1`, "{\n  \"a\": 1\n}\n"},
		{
			"tab indent", "{\n\t\"a\": {\n\t\t\"b\": 1\n\t}\n}\n", "/a/c", `{"d":[1]}`,
			"{\n\t\"a\": {\n\t\t\"b\": 1,\n\t\t\"c\": {\n\t\t\t\"d\": [\n\t\t\t\t1\n\t\t\t]\n\t\t}\n\t}\n}\n",
		},
		{"compact document", `{"a":1}`, "/b", `{"c": [1, 2]}`, `{"a":1,"b":{"c":[1,2]}}`},
		{"replace keeps comments", "{\n  \"a\": /* x */ 1, // y\n  \"b\": 2\n}\n", "/a", `3`, "{\n  \"a\": /* x */ 3, // y\n  \"b\": 2\n}\n"},
		{"trailing comma kept", "{\n  \"a\": 1,\n}\n", "/b", `2`, "{\n  \"a\": 1,\n  \"b\": 2,\n}\n"},
		{"comment stays with its line", "{\n  \"a\": 1 // note\n}\n", "/b", `2`, "{\n  \"a\": 1, // note\n  \"b\": 2\n}\n"},
		{"escaped pointer", "{}\n", "/a~1b/c~0d", `"<&>"`, "{\n  \"a/b\": {\n    \"c~d\": \"<&>\"\n  }\n}\n"},
		{"array index", "{\n  \"a\": [1, 2]\n}\n", "/a/1", `3`, "{\n  \"a\": [1, 3]\n}\n"},
		{"array append", "{\n  \"a\": [1, 2]\n}\n", "/a/-", `3`, "{\n  \"a\": [1, 2, 3]\n}\n"},
		{"equal value untouched", "{\n  \"a\": [1,   2]\n}\n", "/a", `[1,2]`, "{\n  \"a\": [1,   2]\n}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertBytes(t, jsonSet(t, []byte(c.doc), c.key, c.value), c.want)
		})
	}
}

func TestJSONDeleteKeepsNeighboursAndComments(t *testing.T) {
	cases := []struct{ name, doc, key, want string }{
		{"middle", "{\n  \"a\": 1,\n  \"b\": 2,\n  \"c\": 3\n}\n", "/b", "{\n  \"a\": 1,\n  \"c\": 3\n}\n"},
		{"first", "{\n  \"a\": 1,\n  \"b\": 2\n}\n", "/a", "{\n  \"b\": 2\n}\n"},
		{"last", "{\n  \"a\": 1,\n  \"b\": 2\n}\n", "/b", "{\n  \"a\": 1\n}\n"},
		{"only", "{\n  \"a\": 1\n}\n", "/a", "{}\n"},
		{"inline first", `{"a":1, "b":2}`, "/a", `{"b":2}`},
		{"comment above", "{\n  \"a\": 1,\n  // about b\n  \"b\": 2,\n  \"c\": 3\n}\n", "/b", "{\n  \"a\": 1,\n  // about b\n  \"c\": 3\n}\n"},
		{"comment of previous line", "{\n  \"a\": 1, // about a\n  \"b\": 2\n}\n", "/b", "{\n  \"a\": 1 // about a\n}\n"},
		{"comment before closing brace", "{\n  \"a\": 1,\n  \"b\": 2 // about b\n}\n", "/b", "{\n  \"a\": 1 // about b\n}\n"},
		{"comment before comma", "{\n  \"a\": 1 /* x */,\n  \"b\": 2\n}\n", "/b", "{\n  \"a\": 1 /* x */\n}\n"},
		{"line comment never swallows", "{\n  // c\n  \"a\": 1}", "/a", "{\n  // c\n  }"},
		{"array element", "{\n  \"a\": [1, 2, 3]\n}\n", "/a/1", "{\n  \"a\": [1, 3]\n}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertBytes(t, jsonDelete(t, []byte(c.doc), c.key), c.want)
		})
	}
}

func TestJSONDeleteAbsentIsByteIdentical(t *testing.T) {
	doc := fixture(t, "opencode.jsonc")
	keys := []string{
		"/nope", "/mcp/nope", "/nope/deeper", "/model/deeper", "/instructions/9", "/instructions@=nope",
		"/nope@=x", "/model@=x",
	}
	for _, key := range keys {
		assertBytes(t, jsonDelete(t, doc, key), string(doc))
	}
	assertBytes(t, jsonDelete(t, []byte(""), "/a"), "")
}

func TestJSONErrors(t *testing.T) {
	cases := []struct{ name, doc, key, value, mention string }{
		{"empty pointer", `{}`, "", `1`, "whole document"},
		{"not a pointer", `{}`, "a/b", `1`, "a/b"},
		{"scalar parent", `{"a":1}`, "/a/b", `1`, "/a"},
		{"element target not array", `{"a":1}`, "/a@=x", `"x"`, "/a"},
		{"index out of range", `{"a":[]}`, "/a/3", `1`, "/a/3"},
		{"comment in value", `{}`, "/a", "1 // x", "/a"},
		{"trailing comma in value", `{}`, "/a", "[1,]", "/a"},
		{"invalid document", `{"a": secretvalue}`, "/a", `1`, "line 1"},
		{"duplicate member", `{"a":1,"a":2}`, "/a", `3`, "/a"},
		{"scalar root", `1`, "/a", `3`, "/a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := JSONSet([]byte(c.doc), c.key, []byte(c.value))
			if err == nil || !strings.Contains(err.Error(), c.mention) {
				t.Fatalf("want an error mentioning %q, got %v", c.mention, err)
			}
			if strings.Contains(err.Error(), "secretvalue") {
				t.Error("error repeats document content")
			}
		})
	}
	if _, err := JSONDelete([]byte(`{}`), ""); err == nil {
		t.Error("deleting the whole document must fail")
	}
}

func TestJSONCCommentsSurvive(t *testing.T) {
	t.Run("opencode", func(t *testing.T) {
		doc := fixture(t, "opencode.jsonc")
		set := jsonSet(t, doc, "/model", `"example/other"`)
		assertBytes(t, set, replaceOnce(t, doc, `"example/model"`, `"example/other"`))
		set = jsonSet(t, doc, "/mcp/search", `{"type":"remote"}`)
		assertBytes(t, set, replaceOnce(t, doc,
			"\t\t// nothing after docs\n",
			"\t\t// nothing after docs\n\t\t\"search\": {\n\t\t\t\"type\": \"remote\"\n\t\t},\n"))
		deleted := jsonDelete(t, doc, "/mcp/docs")
		assertBytes(t, deleted, replaceOnce(t, doc,
			"\n\t\t\"docs\": {\n\t\t\t\"type\": \"local\",\n\t\t\t\"command\": [\"docs-server\", \"--stdio\"],\n\t\t},", ""))
	})
	t.Run("gemini", func(t *testing.T) {
		doc := fixture(t, "gemini-settings.json")
		set := jsonSet(t, doc, "/general/vimMode", `true`)
		assertBytes(t, set, replaceOnce(t, doc, "false", "true"))
		set = jsonSet(t, doc, "/hooks", `{}`)
		assertBytes(t, set, replaceOnce(t, doc,
			"  }\n  // end of settings\n", "  },\n  // end of settings\n  \"hooks\": {}\n"))
	})
}

func TestJSONElementSetIdempotent(t *testing.T) {
	doc := fixture(t, "settings.json")
	cases := map[string]string{
		"/permissions/allow@=Bash(pwd)":       `"Bash(pwd)"`,
		"/hooks/PreToolUse@/matcher=Write":    `{"matcher":"Write","hooks":[]}`,
		"/hooks/Stop@/matcher=":               `{"matcher":""}`,
		"/hooks/PreToolUse@/matcher=Bash":     `{"matcher":"Bash","hooks":[]}`,
		"/permissions/allow@=Read(./docs/**)": `"Read(./docs/**)"`,
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			once := jsonSet(t, doc, key, value)
			assertBytes(t, jsonSet(t, once, key, value), string(once))
			assertSameJSON(t, mustGet(t, once, key), value)
		})
	}
	appended := jsonSet(t, doc, "/permissions/allow@=Bash(pwd)", `"Bash(pwd)"`)
	assertBytes(t, appended, replaceOnce(t, doc,
		"\"Read(./docs/**)\"\n", "\"Read(./docs/**)\",\n      \"Bash(pwd)\"\n"))
}

func TestJSONElementDeleteOnlyMatching(t *testing.T) {
	doc := []byte("{\n  \"list\": [\n    \"keep\",\n    \"drop\",\n    {\"name\": \"drop\"},\n    // note\n    \"drop\",\n    {\"name\": \"keep\", \"n\": 1}\n  ]\n}\n")
	assertBytes(t, jsonDelete(t, doc, "/list@=drop"),
		"{\n  \"list\": [\n    \"keep\",\n    {\"name\": \"drop\"},\n    // note\n    {\"name\": \"keep\", \"n\": 1}\n  ]\n}\n")
	assertBytes(t, jsonDelete(t, doc, "/list@/name=drop"),
		"{\n  \"list\": [\n    \"keep\",\n    \"drop\",\n    // note\n    \"drop\",\n    {\"name\": \"keep\", \"n\": 1}\n  ]\n}\n")
}

func TestJSONElementHashIdentity(t *testing.T) {
	doc := fixture(t, "settings.json")
	hash, err := manifest.Hash(manifest.UnitJSONElement,
		[]byte(`{"hooks":[{"command":"echo example","type":"command"}],"matcher":"Bash"}`))
	if err != nil {
		t.Fatal(err)
	}
	key := "/hooks/PreToolUse@#" + hash
	mustGet(t, doc, key)
	set := jsonSet(t, doc, key, `{"matcher":"Edit"}`)
	assertBytes(t, set, replaceOnce(t, doc,
		"{\n        \"matcher\": \"Bash\",\n        \"hooks\": [{ \"type\": \"command\", \"command\": \"echo example\" }]\n      }",
		"{\n        \"matcher\": \"Edit\"\n      }"))
	deleted := jsonDelete(t, doc, key)
	if _, found, err := JSONGet(deleted, key); found || err != nil {
		t.Errorf("element still found: %v %v", found, err)
	}
	assertSameJSON(t, mustGet(t, deleted, "/hooks/PreToolUse"), `[]`)
}

const docsBlock = "[mcp_servers.docs]\ncommand = \"other-server\"\n\n[mcp_servers.docs.env]\nLEVEL = \"info\"\n"

func tomlSet(t *testing.T, doc []byte, table, block string) []byte {
	t.Helper()
	out, err := TOMLSetTable(doc, table, []byte(block))
	if err != nil {
		t.Fatalf("set %s: %v", table, err)
	}
	return out
}

func tomlDelete(t *testing.T, doc []byte, table string) []byte {
	t.Helper()
	out, err := TOMLDeleteTable(doc, table)
	if err != nil {
		t.Fatalf("delete %s: %v", table, err)
	}
	return out
}

func TestTOMLGetTableReturnsBlockWithSubtables(t *testing.T) {
	doc := fixture(t, "config.toml")
	for _, table := range []string{"mcp_servers.docs", `mcp_servers."docs"`, "mcp_servers.'docs'"} {
		got, found, err := TOMLGetTable(doc, table)
		if err != nil || !found {
			t.Fatalf("get %s: %v %v", table, found, err)
		}
		assertBytes(t, got, "[mcp_servers.docs]\ncommand = \"docs-server\" # inline note\nargs = [\n  [\"nested\"],\n]\n\n[mcp_servers.docs.env]\nLEVEL = \"debug\"\n")
	}
	if _, found, err := TOMLGetTable(doc, "mcp_servers.fake"); found || err != nil {
		t.Errorf("a header inside a string is not a table: %v %v", found, err)
	}
}

func TestTOMLSetReplacesOnlyBlock(t *testing.T) {
	doc := fixture(t, "config.toml")
	before, rest, _ := strings.Cut(string(doc), "[mcp_servers.docs]\n")
	_, after, _ := strings.Cut(rest, "LEVEL = \"debug\"\n")
	got := tomlSet(t, doc, "mcp_servers.docs", docsBlock)
	assertBytes(t, got, before+docsBlock+after)
	for _, kept := range []string{`[projects."/home/user/app"]`, "# hook trust, written by the tool itself\n[hooks.state.", "[[skills.config]]"} {
		if !strings.Contains(after+before, kept) {
			t.Fatalf("fixture lost %q", kept)
		}
	}
	assertBytes(t, tomlSet(t, got, "mcp_servers.docs", docsBlock+"\n\n"), string(got))
}

func TestTOMLSetEveryExistingTableUnchanged(t *testing.T) {
	doc := fixture(t, "config.toml")
	tables := []string{
		`projects."/home/user/app"`, "mcp_servers.docs", "mcp_servers.docs.env",
		`hooks.state."/home/user/app/.codex/hooks.json:pre_tool_use"`,
	}
	for _, table := range tables {
		block, found, err := TOMLGetTable(doc, table)
		if err != nil || !found {
			t.Fatalf("get %s: %v %v", table, found, err)
		}
		assertBytes(t, tomlSet(t, doc, table, string(block)), string(doc))
	}
}

func TestTOMLSetThenDeleteRestoresBytes(t *testing.T) {
	fixtureDoc := string(fixture(t, "config.toml"))
	docs := []string{
		fixtureDoc, strings.TrimSuffix(fixtureDoc, "\n"), "", "a = 1\n", "a = 1", "a = 1\n\n",
		"[x]\n# trailing comment\n", "a = 1\r\n", "\n",
	}
	block := "[mcp_servers.search]\ncommand = \"search-server\"\n"
	for _, doc := range docs {
		set := tomlSet(t, []byte(doc), "mcp_servers.search", block)
		var parsed map[string]any
		if err := toml.Unmarshal(set, &parsed); err != nil {
			t.Fatalf("result does not parse: %v\n%s", err, set)
		}
		assertBytes(t, tomlDelete(t, set, "mcp_servers.search"), doc)
	}
}

func TestTOMLAppendLayout(t *testing.T) {
	assertBytes(t, tomlSet(t, []byte("a = 1\n"), "t", "[t]\nb = 2"), "a = 1\n\n[t]\nb = 2\n")
	assertBytes(t, tomlSet(t, []byte("a = 1"), "t", "[t]\nb = 2\n"), "a = 1\n\n[t]\nb = 2")
	assertBytes(t, tomlSet(t, nil, "t", "[t]\nb = 2\n"), "[t]\nb = 2\n")
}

func TestTOMLDeleteKeepsCommentsOfNeighbours(t *testing.T) {
	doc := fixture(t, "config.toml")
	got := tomlDelete(t, doc, "mcp_servers.docs")
	before, rest, _ := strings.Cut(string(doc), "[mcp_servers.docs]\n")
	_, after, _ := strings.Cut(rest, "LEVEL = \"debug\"\n")
	assertBytes(t, got, before+after)
	assertBytes(t, tomlDelete(t, []byte("[a]\n\n[t]\nk = 1\n\n[b]\n"), "t"), "[a]\n\n[b]\n")
	assertBytes(t, tomlDelete(t, []byte("[a]\n\n[t]\nk = 1\n# b\n[b]\n"), "t"), "[a]\n\n# b\n[b]\n")
	assertBytes(t, tomlDelete(t, doc, "mcp_servers.nope"), string(doc))
}

func TestTOMLDottedKeyDefinitionUnsupported(t *testing.T) {
	cases := []struct{ name, doc, table string }{
		{"inline parent", `mcp_servers = { docs = { command = "x" } }`, "mcp_servers.docs"},
		{"inline parent without target", `mcp_servers = {}`, "mcp_servers.docs"},
		{"inline target", "[mcp_servers]\ndocs = { command = \"x\" }\n", "mcp_servers.docs"},
		{"dotted target", `mcp_servers.docs.command = "x"`, "mcp_servers.docs"},
		{"dotted sibling under parent", `mcp_servers.other.command = "x"`, "mcp_servers.docs"},
		{"dotted inside parent table", "[mcp_servers]\ndocs.command = \"x\"\n", "mcp_servers.docs"},
		{"dotted subtable of target", "[mcp_servers]\ndocs.env.A = \"x\"\n", "mcp_servers.docs"},
		{"scalar parent", `mcp_servers = 1`, "mcp_servers.docs"},
		{"array of tables target", "[[skills.config]]\npath = \"p\"\n", "skills.config"},
		{"array of tables parent", "[[skills]]\nname = \"p\"\n", "skills.config"},
		{"subtable only", "[mcp_servers.docs.env]\nA = \"x\"\n", "mcp_servers.docs"},
		{"split subtable", "[mcp_servers.docs]\n[other]\n[mcp_servers.docs.env]\n", "mcp_servers.docs"},
		{"subtable first", "[mcp_servers.docs.env]\n[mcp_servers.docs]\n", "mcp_servers.docs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			block := "[" + c.table + "]\ncommand = \"y\"\n"
			if _, err := TOMLSetTable([]byte(c.doc), c.table, []byte(block)); !errors.Is(err, ErrUnsupportedLayout) {
				t.Errorf("set: %v", err)
			}
			if _, err := TOMLDeleteTable([]byte(c.doc), c.table); !errors.Is(err, ErrUnsupportedLayout) {
				t.Errorf("delete: %v", err)
			}
			if _, _, err := TOMLGetTable([]byte(c.doc), c.table); !errors.Is(err, ErrUnsupportedLayout) {
				t.Errorf("get: %v", err)
			}
		})
	}
}

func TestTOMLSetRejectsBadInput(t *testing.T) {
	cases := []struct{ name, doc, table, block string }{
		{"invalid block", "", "t", "[t]\nk = secretvalue\n"},
		{"invalid document", "k = secretvalue\n", "t", "[t]\n"},
		{"other table", "", "t", "[u]\n"},
		{"foreign second table", "", "t", "[t]\n[u]\n"},
		{"root keys first", "", "t", "k = 1\n[t]\n"},
		{"leading comment", "", "t", "# c\n[t]\n"},
		{"trailing comment", "", "t", "[t]\n# c\n"},
		{"array header", "", "t", "[[t]]\n"},
		{"empty block", "", "t", ""},
		{"bad table path", "", "t.", "[t]\n"},
		{"empty table path", "", "", "[t]\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := TOMLSetTable([]byte(c.doc), c.table, []byte(c.block))
			if err == nil {
				t.Fatal("want an error")
			}
			if strings.Contains(err.Error(), "secretvalue") {
				t.Error("error repeats content")
			}
		})
	}
}
