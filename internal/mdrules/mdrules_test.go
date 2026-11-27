package mdrules_test

import (
	"reflect"
	"testing"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/mdrules"
)

const source = "/work/app/AGENTS.md"

func rule(name string, order int, body string) library.Item {
	return library.Item{Kind: library.KindRule, Name: name, Order: order, Body: body, Source: source}
}

func TestSplit(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want []library.Item
	}{
		{
			"preamble and sections",
			"# App\n\nIntro.\n\n## Build\n\nRun make.\n\n\n## Style\nTabs.\n### Nested\n\nKept.\n\n",
			[]library.Item{
				rule("AGENTS", 10, "# App\n\nIntro.\n"),
				rule("Build", 20, "## Build\n\nRun make.\n"),
				rule("Style", 30, "## Style\nTabs.\n### Nested\n\nKept.\n"),
			},
		},
		{
			"whitespace preamble skipped",
			"\n  \n## Only\n\nText.",
			[]library.Item{rule("Only", 10, "## Only\n\nText.\n")},
		},
		{
			"headings inside fences",
			"## A\n\n```sh\n## not a heading\n```\n\n~~~~\n~~~\n## still fenced\n~~~~\n\n## B\n\n``` inline ``` text\n\n## C\n",
			[]library.Item{
				rule("A", 10, "## A\n\n```sh\n## not a heading\n```\n\n~~~~\n~~~\n## still fenced\n~~~~\n"),
				rule("B", 20, "## B\n\n``` inline ``` text\n"),
				rule("C", 30, "## C\n"),
			},
		},
		{
			"managed markers and notice dropped",
			managed.Begin + "\n" + managed.Notice + "\n## A\n\nText.\n" + managed.End + "\n",
			[]library.Item{rule("A", 10, "## A\n\nText.\n")},
		},
		{
			"byte order mark and CRLF",
			"\xef\xbb\xbf## A\r\n\r\nText.\r\n",
			[]library.Item{rule("A", 10, "## A\n\nText.\n")},
		},
		{
			"empty heading text is not a boundary",
			"## A\n\n## \nText.\n",
			[]library.Item{rule("A", 10, "## A\n\n## \nText.\n")},
		},
		{"empty document", "", nil},
		{"only an empty section", managed.Begin + "\n" + managed.Notice + "\n\n" + managed.End + "\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mdrules.Split([]byte(c.doc), source); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v\nwant %#v", got, c.want)
			}
		})
	}
}

func TestRender(t *testing.T) {
	items := []library.Item{
		{Body: "Intro."},
		{Body: "\n## A\n\nText.\n\n\n"},
		{Body: " \n"},
		{Body: "## B\n"},
	}
	if got, want := string(mdrules.Render(items)), "Intro.\n\n## A\n\nText.\n\n## B\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := mdrules.Render(nil); len(got) != 0 {
		t.Errorf("no items render %q", got)
	}
}

func TestSplitThenRenderRoundTrips(t *testing.T) {
	doc := "# App\n\nIntro.\n\n## Build\n\n    indented code\n\n```\n## fenced\n```\n\n## Style\n\nTabs.\n"
	if got := string(mdrules.Render(mdrules.Split([]byte(doc), source))); got != doc {
		t.Errorf("got %q, want %q", got, doc)
	}
}
