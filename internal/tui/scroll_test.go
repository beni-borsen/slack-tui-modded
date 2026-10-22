package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func press(m *Model, k string) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	m.View()
}

// tallList opens C2 with a short message, one of 40 numbered lines (taller
// than the pane) and another short one, and focuses the messages.
func tallList(t *testing.T, last bool) (*Model, *msgList) {
	t.Helper()
	m := fakeModel(t, 100, 20)
	m.open("C2")
	var rows []string
	for i := 1; i <= 40; i++ {
		rows = append(rows, fmt.Sprintf("row%02d", i))
	}
	msgs := []backend.Message{
		{TS: "100.000001", User: "U1", Text: "before"},
		{TS: "100.000002", User: "U2", Text: strings.Join(rows, "\n")},
	}
	if !last {
		msgs = append(msgs, backend.Message{TS: "100.000003", User: "U1", Text: "after"})
	}
	m.Update(historyMsg{id: "C2", msgs: msgs})
	m.focus = fMessages
	m.View()
	return m, m.lists["C2"]
}

// rowsShown is which numbered rows of the tall message are on screen.
func rowsShown(t *testing.T, m *Model) (first, last int) {
	t.Helper()
	for _, l := range strings.Split(dump(t, m), "\n") {
		var n int
		if i := strings.Index(l, "row"); i >= 0 {
			if _, err := fmt.Sscanf(l[i:], "row%02d", &n); err == nil {
				if first == 0 {
					first = n
				}
				last = n
			}
		}
	}
	return first, last
}

func TestTallMessageScrollsByLines(t *testing.T) {
	m, l := tallList(t, false)
	press(m, "k") // onto the tall message from below: its end shows
	if l.sel != 1 {
		t.Fatalf("sel = %d, want 1", l.sel)
	}
	if _, last := rowsShown(t, m); last != 40 {
		t.Errorf("coming from below shows up to row %d, want 40", last)
	}
	if v := dump(t, m); !strings.Contains(v, "Bob") || !strings.Contains(v, "more lines") {
		t.Errorf("no sticky sender/hint over a message cut off above")
	}
	// Scrolling up through it shows every row before moving on.
	seen := map[int]bool{}
	for range 30 {
		if l.sel != 1 {
			break
		}
		first, last := rowsShown(t, m)
		for i := first; i <= last; i++ {
			seen[i] = true
		}
		press(m, "k")
	}
	for i := 1; i <= 40; i++ {
		if !seen[i] {
			t.Errorf("row %d never shown scrolling up", i)
		}
	}
	if l.sel != 0 {
		t.Errorf("sel = %d after scrolling past the top, want 0", l.sel)
	}
	press(m, "j") // onto it from above: its start shows
	if first, _ := rowsShown(t, m); first != 1 || !strings.Contains(dump(t, m), "more lines") {
		t.Errorf("coming from above shows from row %d, want 1 and a hint below", first)
	}
}

func TestTallLastMessageFollows(t *testing.T) {
	m, l := tallList(t, true)
	if _, last := rowsShown(t, m); last != 40 || l.sel != -1 {
		t.Fatalf("following shows up to row %d (sel %d), want 40 and -1", last, l.sel)
	}
	press(m, "k") // scrolls within it instead of leaving it
	if l.sel != 1 {
		t.Errorf("k in a tall last message: sel = %d, want 1", l.sel)
	}
	if _, last := rowsShown(t, m); last >= 40 {
		t.Errorf("k didn't scroll up: last row %d", last)
	}
	for range 30 {
		press(m, "j")
	}
	if l.sel != -1 {
		t.Errorf("back at its end: sel = %d, want -1 (following)", l.sel)
	}
}

func TestHalfPage(t *testing.T) {
	m, l := tallList(t, false)
	press(m, "g")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m.View()
	if l.sel != 1 {
		t.Errorf("ctrl+d from the top: sel = %d, want 1 (the tall message)", l.sel)
	}
	first, _ := rowsShown(t, m)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m.View()
	if f, _ := rowsShown(t, m); f <= first {
		t.Errorf("second ctrl+d didn't move through the message: row %d, was %d", f, first)
	}
	for range 10 {
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
		m.View()
	}
	if l.sel != -1 {
		t.Errorf("ctrl+d to the bottom: sel = %d, want -1", l.sel)
	}
	for range 10 {
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
		m.View()
	}
	if l.sel != 0 || l.top != 0 {
		t.Errorf("ctrl+u to the top: sel %d top %d, want 0 0", l.sel, l.top)
	}
}

func TestHalfPageShortMessages(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.open("C2")
	var msgs []backend.Message
	for i := range 60 {
		msgs = append(msgs, backend.Message{TS: fmt.Sprintf("100.%06d", i+1), User: []string{"U1", "U2"}[i%2], Text: fmt.Sprintf("message %d", i)})
	}
	m.Update(historyMsg{id: "C2", msgs: msgs})
	m.focus = fMessages
	m.View()
	l := m.lists["C2"]
	press(m, "g")
	prev := l.top
	for i := 0; l.sel != -1; i++ {
		if i > 40 {
			t.Fatalf("ctrl+d never reached the bottom (top %d)", l.top)
		}
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
		m.View()
		if l.sel != -1 && l.top <= prev {
			t.Fatalf("ctrl+d didn't move down: top %d, was %d", l.top, prev)
		}
		prev = l.top
	}
}
