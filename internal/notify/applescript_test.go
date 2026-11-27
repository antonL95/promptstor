package notify

import "testing"

func TestAppleScriptEscapesQuotesAndBackslashes(t *testing.T) {
	got := appleScript(`say "hi" \ " & do shell script "x`)
	want := `display notification "say \"hi\" \\ \" & do shell script \"x" with title "PromptStor"`
	if got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}
