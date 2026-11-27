package library

import (
	"reflect"
	"testing"
)

func TestParseMarkdownKeepsFrontmatterOrder(t *testing.T) {
	doc := []byte("---\ndescription: Review a PR\nmodel: sonnet\nargument-hint: \"[pr]\"\n---\nReview $0.\n")
	it, err := ParseMarkdown(KindCommand, "review", doc)
	if err != nil {
		t.Fatal(err)
	}
	if it.Name != "review" || it.Description != "Review a PR" || it.Body != "Review $0.\n" {
		t.Errorf("item %+v", it)
	}
	if got := it.MetaKeys(); !reflect.DeepEqual(got, []string{"model", "argument-hint"}) {
		t.Errorf("meta keys %v", got)
	}
}

func TestParseMarkdownRejectsYAMLKinds(t *testing.T) {
	if _, err := ParseMarkdown(KindMCP, "x", []byte("command: x\n")); err == nil {
		t.Error("no error for a YAML kind")
	}
}
