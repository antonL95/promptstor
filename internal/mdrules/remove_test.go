package mdrules_test

import (
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/mdimport"
	"github.com/antonL95/promptstor/internal/mdrules"
)

const handWritten = "# Personal\n\n@~/notes/review.md\n\n## Testing\n\nRun the suite.\n\n<laravel-boost-guidelines>\n## Boost\nGenerated.\n</laravel-boost-guidelines>\n\nAlways green.\n\n## Style\n\nShort names.\n\n" +
	"<!-- promptstor:begin -->\n## Testing\n\nRun the suite.\n<!-- promptstor:end -->\n"

func importedRules(t *testing.T, doc string, names ...string) []library.Item {
	t.Helper()
	text := strings.Replace(doc, "<laravel-boost-guidelines>\n## Boost\nGenerated.\n</laravel-boost-guidelines>\n", "", 1)
	var picked []library.Item
	for _, it := range mdrules.Split(mdimport.StripImportLines([]byte(text), mdimport.Claude), source) {
		for _, name := range names {
			if it.Name == name {
				picked = append(picked, it)
			}
		}
	}
	if len(picked) != len(names) {
		t.Fatalf("fixture splits into other rules: %v", picked)
	}
	return picked
}

func TestRemoveKeepsBoostBlocksImportsAndOtherRules(t *testing.T) {
	doc := strings.SplitAfter(handWritten, "<!-- promptstor:begin -->")[0]
	doc = strings.TrimSuffix(doc, "<!-- promptstor:begin -->")
	got, err := mdrules.Remove([]byte(handWritten), importedRules(t, doc, "Testing"))
	if err != nil {
		t.Fatal(err)
	}
	want := "# Personal\n\n@~/notes/review.md\n\n<laravel-boost-guidelines>\n## Boost\nGenerated.\n</laravel-boost-guidelines>\n\n## Style\n\nShort names.\n\n" +
		"<!-- promptstor:begin -->\n## Testing\n\nRun the suite.\n<!-- promptstor:end -->\n"
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRemoveEveryRuleLeavesOnlyUnimportedText(t *testing.T) {
	doc := "# Personal\n\n@~/notes/review.md\n\n## Testing\n\nRun the suite.\n\n## Style\n\nShort names.\n"
	got, err := mdrules.Remove([]byte(doc), importedRules(t, doc, "Testing", "Style"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Personal\n\n@~/notes/review.md\n\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRemoveRefusesRulesNotInTheFile(t *testing.T) {
	doc := "## Testing\n\nRun the suite.\n"
	missing := library.Item{Kind: library.KindRule, Name: "Style", Body: "## Style\n\nShort names.\n"}
	if _, err := mdrules.Remove([]byte(doc), []library.Item{missing}); err == nil {
		t.Fatal("removed a rule the file does not hold")
	}
}
