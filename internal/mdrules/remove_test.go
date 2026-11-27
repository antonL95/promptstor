package mdrules_test

import (
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/mdimport"
	"github.com/antonL95/promptstor/internal/mdrules"
)

const (
	boostBegin = "<laravel-boost-guidelines>"
	boostEnd   = "</laravel-boost-guidelines>"
)

var claudeOptions = mdrules.RemoveOptions{
	Opaque: []mdrules.Block{{Begin: boostBegin, End: boostEnd}},
	Keep:   func(line string) bool { return mdimport.OnlyImports(line, mdimport.Claude) },
}

func withoutBoost(doc string) string {
	start := strings.Index(doc, boostBegin)
	end := strings.Index(doc, boostEnd)
	if start < 0 || end < 0 {
		return doc
	}
	return doc[:start] + doc[end+len(boostEnd)+1:]
}

func importedRules(t *testing.T, doc string, names ...string) []library.Item {
	t.Helper()
	all := mdrules.Split(mdimport.StripImportLines([]byte(withoutBoost(doc)), mdimport.Claude), source)
	var picked []library.Item
	for _, name := range names {
		for _, it := range all {
			if it.Name == name {
				picked = append(picked, it)
				break
			}
		}
	}
	if len(picked) != len(names) {
		t.Fatalf("fixture splits into other rules: %v", picked)
	}
	return picked
}

func TestRemoveKeepsBoostBlocksImportsAndOtherRules(t *testing.T) {
	doc := "# Personal\n\n@~/notes/review.md\n\n## Testing\n\nRun the suite.\n\n" + boostBegin + "\n## Boost\nGenerated.\n" + boostEnd + "\n\nAlways green.\n\n## Style\n\nShort names.\n\n" +
		"<!-- promptstor:begin -->\n## Testing\n\nRun the suite.\n<!-- promptstor:end -->\n"
	got, err := mdrules.Remove([]byte(doc), source, importedRules(t, doc, "Testing"), claudeOptions)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Personal\n\n@~/notes/review.md\n\n" + boostBegin + "\n## Boost\nGenerated.\n" + boostEnd + "\n## Style\n\nShort names.\n\n" +
		"<!-- promptstor:begin -->\n## Testing\n\nRun the suite.\n<!-- promptstor:end -->\n"
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRemoveEveryRuleLeavesOnlyUnimportedText(t *testing.T) {
	doc := "# Personal\n\n@~/notes/review.md\n\n## Testing\n\nRun the suite.\n@~/notes/testing.md\n\n## Style\n\nShort names.\n"
	got, err := mdrules.Remove([]byte(doc), source, importedRules(t, doc, "Testing", "Style"), claudeOptions)
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Personal\n\n@~/notes/review.md\n\n@~/notes/testing.md\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRemovalUsesSpansNotText(t *testing.T) {
	doc := boostBegin + "\n## Testing\n\nRun the suite.\n" + boostEnd + "\n\n## Testing\n\nRun the suite.\n\n## Style\n\nRun the suite.\n"
	got, err := mdrules.Remove([]byte(doc), source, importedRules(t, doc, "Testing"), claudeOptions)
	if err != nil {
		t.Fatal(err)
	}
	want := boostBegin + "\n## Testing\n\nRun the suite.\n" + boostEnd + "\n\n## Style\n\nRun the suite.\n"
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRemoveRefusesRulesNotInTheFile(t *testing.T) {
	doc := "## Testing\n\nRun the suite.\n"
	for _, missing := range []library.Item{
		{Kind: library.KindRule, Name: "Style", Order: 10, Body: "## Style\n\nShort names.\n"},
		{Kind: library.KindRule, Name: "Testing", Order: 10, Body: "## Testing\n\nRun it twice.\n"},
	} {
		if _, err := mdrules.Remove([]byte(doc), source, []library.Item{missing}, mdrules.RemoveOptions{}); err == nil {
			t.Errorf("removed %q although the file does not hold it", missing.Body)
		}
	}
}
