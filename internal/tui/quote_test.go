package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// A long quote keeps its bar on every wrapped row, and the reply under it
// has none.
func TestQuoteBarOnEveryRow(t *testing.T) {
	m := fakeModel(t, 100, 24)
	text := render.Mrkdwn("&gt; It seems however that our api keys to staging2 has expired so if you can get me a new one\nOn it", m, mdStyles)
	rows := wrap(text, 30)
	if len(rows) < 4 {
		t.Fatalf("expected the quote to wrap: %q", rows)
	}
	for i, r := range rows[:len(rows)-1] {
		if !strings.HasPrefix(ansi.Strip(r), "▎ ") {
			t.Errorf("row %d lost the quote bar: %q", i, ansi.Strip(r))
		}
	}
	if last := ansi.Strip(rows[len(rows)-1]); last != "On it" {
		t.Errorf("reply row = %q", last)
	}
}
