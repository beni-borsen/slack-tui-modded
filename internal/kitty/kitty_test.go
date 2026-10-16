package kitty

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPlaceholder(t *testing.T) {
	lines := Placeholder(0x5A0102, 5, 3)
	if len(lines) != 3 {
		t.Fatalf("rows = %d", len(lines))
	}
	for r, l := range lines {
		if w := ansi.StringWidth(l); w != 5 {
			t.Errorf("row %d is %d cells wide, want 5", r, w)
		}
		if !strings.HasPrefix(l, "\x1b[38;2;90;1;2m") {
			t.Errorf("row %d does not carry the id in its colour: %q", r, l)
		}
		if !strings.Contains(l, string(placeholder)+string(diacritics[r])+string(diacritics[0])) {
			t.Errorf("row %d lacks its row/column marks", r)
		}
	}
}

func TestTmuxWrap(t *testing.T) {
	w := &Writer{tmux: true}
	got := w.command("a=d,i=1", "")
	want := "\x1bPtmux;\x1b\x1b_Ga=d,i=1\x1b\x1b\\\x1b\\"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
