package tui

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestToggleMark(t *testing.T) {
	m := chanWithMessages(t)
	m.Update(key("i"))
	m.Update(key("hello world"))
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if got := m.input.Value(); got != "hello *world*" {
		t.Fatalf("bold: %q", got)
	}
	m.Update(key("!"))
	if got := m.input.Value(); got != "hello *world*!" {
		t.Errorf("cursor should sit after the closing mark: %q", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if got := m.input.Value(); got != "hello world" {
		t.Errorf("second ctrl+b should unwrap: %q", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i"), Alt: true})
	if got := m.input.Value(); got != "_hello_ world" {
		t.Errorf("alt+i on the first word: %q", got)
	}
	m.input.Reset()
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	m.Update(key("x"))
	if got := m.input.Value(); got != "*x*" {
		t.Errorf("empty: %q", got)
	}
}

func TestWrappedRowsMatchesTextarea(t *testing.T) {
	words := []string{"a", "hej", "verylongwordthatgoesonandon", "ø", "中文", "👍", "  ", "x"}
	for i := range 400 {
		w := 5 + i%30
		var b strings.Builder
		for range rand.IntN(25) {
			b.WriteString(words[rand.IntN(len(words))])
			if rand.IntN(3) > 0 {
				b.WriteString(" ")
			}
		}
		s := b.String()
		ta := textarea.New()
		ta.ShowLineNumbers, ta.Prompt, ta.EndOfBufferCharacter = false, "", '~'
		ta.SetWidth(w)
		ta.SetHeight(60)
		ta.SetValue(s)
		got := wrappedRows([]rune(s), ta.Width())
		want := 0
		for _, l := range strings.Split(ansi.Strip(ta.View()), "\n") {
			if strings.HasPrefix(l, "~") {
				break
			}
			want++
		}
		if got != want {
			t.Fatalf("width %d %q: counted %d rows, textarea draws %d", ta.Width(), s, got, want)
		}
	}
}

func TestInputGrowsWithWrapping(t *testing.T) {
	m := chanWithMessages(t)
	m.Update(key("i"))
	m.Update(key(strings.Repeat("word ", 60))) // ~300 chars in a ~110 wide box
	dump(t, m)
	if h := m.input.Height(); h < 3 {
		t.Errorf("input stayed %d rows for wrapped text", h)
	}
	m.Update(key(strings.Repeat("more ", 400)))
	dump(t, m)
	if h, limit := m.input.Height(), min(8, m.height/3); h != limit {
		t.Errorf("input is %d rows, want the %d-row limit", h, limit)
	}
}
