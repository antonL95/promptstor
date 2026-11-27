package preview

import "testing"

func TestUnifiedDiff(t *testing.T) {
	before := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\ntwelve\n"
	after := "one\ntwo\nthree\nFOUR\nfive\nsix\nseven\neight\nnine\nten\neleven\ntwelve\nthirteen"
	want := "--- /x/file\n+++ /x/file\n" +
		"@@ -1,7 +1,7 @@\n one\n two\n three\n-four\n+FOUR\n five\n six\n seven\n" +
		"@@ -10,3 +10,4 @@\n ten\n eleven\n twelve\n+thirteen\n\\ No newline at end of file\n"
	if got := UnifiedDiff("/x/file", []byte(before), []byte(after)); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestUnifiedDiffNewFile(t *testing.T) {
	want := "--- /x/file\n+++ /x/file\n@@ -0,0 +1,2 @@\n+a\n+b\n"
	if got := UnifiedDiff("/x/file", nil, []byte("a\nb\n")); got != want {
		t.Errorf("got\n%s", got)
	}
}

func TestUnifiedDiffDeletedFile(t *testing.T) {
	want := "--- /x/file\n+++ /x/file\n@@ -1,1 +0,0 @@\n-a\n"
	if got := UnifiedDiff("/x/file", []byte("a\n"), nil); got != want {
		t.Errorf("got\n%s", got)
	}
}
