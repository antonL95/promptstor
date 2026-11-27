package managed

import (
	"errors"
	"testing"
)

const section = Begin + "\n" + Notice + "\nbody\n" + End

func mustSet(t *testing.T, doc, body string) string {
	t.Helper()
	got, err := SetSection([]byte(doc), []byte(body))
	if err != nil {
		t.Fatalf("SetSection(%q): %v", doc, err)
	}
	return string(got)
}

func TestSetSectionAppendsWhenAbsent(t *testing.T) {
	tests := []struct{ name, doc, body, want string }{
		{"empty document", "", "body", section + "\n"},
		{"newline terminated", "# Mine\n", "body\n", "# Mine\n\n" + section + "\n"},
		{"unterminated", "# Mine", "body", "# Mine\n\n" + section + "\n"},
		{"already blank line", "# Mine\n\n", "body", "# Mine\n\n" + section + "\n"},
		{"extra blank lines stay", "# Mine\n\n\n\n", "body", "# Mine\n\n\n\n" + section + "\n"},
		{"crlf blank line", "# Mine\r\n\r\n", "body", "# Mine\r\n\r\n" + section + "\n"},
		{"body newlines collapse", "", "body\n\n\n", section + "\n"},
		{"only a blank line", "\n", "body", "\n" + section + "\n"},
		{"whitespace only last line", "# Mine\n \t\n", "body", "# Mine\n \t\n" + section + "\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := mustSet(t, test.doc, test.body); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestSetSectionKeepsSurroundingBytes(t *testing.T) {
	old := Begin + "\n" + Notice + "\nold\n" + End
	tests := []struct{ name, before, after string }{
		{"text around", "# Top  \n\n\n", "\n\n\tTail \n"},
		{"crlf", "# Top\r\n", "\r\nTail\r\n"},
		{"end unterminated", "# Top\n", ""},
		{"trailing spaces after end", "", "  \nTail"},
		{"byte order mark", "\ufeff", "\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := mustSet(t, test.before+old+test.after, "body")
			if want := test.before + section + test.after; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestSectionReturnsBodyWithoutNotice(t *testing.T) {
	tests := []struct {
		name, doc, want string
		found           bool
	}{
		{"absent", "# Mine\n", "", false},
		{"present", "a\n" + section + "\nb\n", "body\n", true},
		{"notice removed by hand", Begin + "\nbody\n" + End + "\n", "body\n", true},
		{"inline mention is not a marker", "use `" + Begin + "` here\n", "", false},
		{"fenced markers count", "```\n" + section + "\n```\n", "body\n", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, found, err := Section([]byte(test.doc))
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != test.want || found != test.found {
				t.Errorf("got %q %v, want %q %v", body, found, test.want, test.found)
			}
		})
	}
}

func TestUnbalancedMarkersError(t *testing.T) {
	tests := map[string]string{
		"begin only":   Begin + "\nx\n",
		"end only":     "x\n" + End + "\n",
		"reversed":     End + "\n" + Begin + "\n",
		"two sections": section + "\n" + section + "\n",
		"nested":       Begin + "\n" + Begin + "\n" + End + "\n",
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Section([]byte(doc)); !errors.Is(err, ErrMarkers) {
				t.Errorf("Section: got %v", err)
			}
			if got, err := SetSection([]byte(doc), []byte("b")); !errors.Is(err, ErrMarkers) || got != nil {
				t.Errorf("SetSection: got %q %v", got, err)
			}
			if got, err := RemoveSection([]byte(doc)); !errors.Is(err, ErrMarkers) || got != nil {
				t.Errorf("RemoveSection: got %q %v", got, err)
			}
		})
	}
}

func TestSetSectionRefusesMarkerInBody(t *testing.T) {
	for _, body := range []string{Begin, "a\n" + End + "\nb"} {
		if got, err := SetSection(nil, []byte(body)); !errors.Is(err, ErrMarkers) || got != nil {
			t.Errorf("body %q: got %q %v", body, got, err)
		}
	}
}

func TestRemoveSectionRemovesOnlyItsLines(t *testing.T) {
	tests := []struct{ name, doc, want string }{
		{"absent", "# Mine\n", "# Mine\n"},
		{"middle", "a\n\n" + section + "\n\nb\n", "a\n\n\nb\n"},
		{"crlf", "a\r\n" + section + "\r\nb\r\n", "a\r\nb\r\n"},
		{"unterminated", "a\n" + section, "a\n"},
		{"only section", section + "\n", ""},
		{"byte order mark stays", "\ufeff" + section + "\nb\n", "\ufeffb\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := RemoveSection([]byte(test.doc))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestSectionEditsLeaveInputUntouched(t *testing.T) {
	original := "a\n" + section + "\nb\n"
	doc := []byte(original)
	if _, err := SetSection(doc, []byte("a much longer replacement body")); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveSection(doc); err != nil {
		t.Fatal(err)
	}
	if string(doc) != original {
		t.Errorf("input changed to %q", doc)
	}
}

func TestNoticeAfterFrontmatter(t *testing.T) {
	tests := []struct{ name, content, want string }{
		{"frontmatter", "---\nname: x\n---\nbody\n", "---\nname: x\n---\n" + Notice + "\nbody\n"},
		{"empty frontmatter", "---\n---\nbody\n", "---\n---\n" + Notice + "\nbody\n"},
		{"crlf frontmatter", "---\r\na: 1\r\n---\r\nbody\r\n", "---\r\na: 1\r\n---\r\n" + Notice + "\nbody\r\n"},
		{"closing unterminated", "---\na: 1\n---", "---\na: 1\n---\n" + Notice + "\n"},
		{"no frontmatter", "body\n", Notice + "\nbody\n"},
		{"unclosed frontmatter", "---\nbody\n", Notice + "\n---\nbody\n"},
		{"empty", "", Notice + "\n"},
		{"byte order mark", "\ufeffbody\n", "\ufeff" + Notice + "\nbody\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			once := WithNotice([]byte(test.content))
			if string(once) != test.want {
				t.Fatalf("got %q, want %q", once, test.want)
			}
			if twice := WithNotice(once); string(twice) != test.want {
				t.Errorf("second call gave %q", twice)
			}
		})
	}
}

func TestStripNoticeUndoesWithNotice(t *testing.T) {
	for _, content := range []string{"", "body\n", "---\na: 1\n---\nbody\n", "\ufeffbody\n", "text\n" + Notice + "\n"} {
		if got := StripNotice(WithNotice([]byte(content))); string(got) != content {
			t.Errorf("got %q, want %q", got, content)
		}
	}
	if got := StripNotice([]byte("text\n" + Notice + "\n")); string(got) != "text\n"+Notice+"\n" {
		t.Errorf("notice away from the top was removed: %q", got)
	}
}
