package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeIn(m *Model, s string) {
	for _, r := range s {
		m.Update(key(string(r)))
	}
}

func helpTop(m *Model) helpEntry { return m.help.entries[m.help.matches[0]] }

func TestHelpFilter(t *testing.T) {
	m := fakeModel(t, 120, 30)
	m.Update(key("?"))
	if m.help == nil {
		t.Fatal("? should open the help")
	}
	v := dump(t, m)
	if !strings.Contains(v, "jump to conversation") || !strings.Contains(v, "of ") {
		t.Error("the full list should show, scrolling")
	}

	typeIn(m, "D")
	if e := helpTop(m); e.key != "D" {
		t.Errorf("typing a key should find it first, got %q", e.key)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	typeIn(m, "pin")
	if m.help.input.Value() != "pin" {
		t.Fatalf("filter %q", m.help.input.Value())
	}
	if e := helpTop(m); !strings.Contains(e.desc, "pin") {
		t.Errorf("\"pin\": first %q %q", e.key, e.desc)
	}
	m.help.input.SetValue("")
	typeIn(m, "save down")
	if e := helpTop(m); e.key != "s" {
		t.Errorf("\"save down\": first %q", e.key)
	}
	m.help.input.SetValue("")
	typeIn(m, "zzqx")
	if len(m.help.matches) != 0 || !strings.Contains(dump(t, m), "nothing matches") {
		t.Error("no match should say so")
	}

	// ? types into a filter; esc closes.
	typeIn(m, "?")
	if m.help == nil {
		t.Fatal("? with a filter should type, not close")
	}
	m.Update(key("esc"))
	if m.help != nil {
		t.Fatal("esc should close")
	}

	// enter on a command starts it.
	m.Update(key("?"))
	typeIn(m, "dnd")
	m.Update(key("enter"))
	if m.help != nil || m.cmd == nil || m.cmd.input.Value() != "dnd " {
		t.Errorf("enter on :dnd: cmd %+v", m.cmd)
	}
	m.Update(key("esc"))
	m.Update(key("?"))
	m.Update(key("?"))
	if m.help != nil {
		t.Error("? on an empty filter should close")
	}
}

func TestHelpRanking(t *testing.T) {
	m := fakeModel(t, 120, 30)
	m.Update(key("?"))
	for q, want := range map[string]string{
		"read": "r", "image": "ctrl+v  (insert)", "status": ":status", "copy": "c", "P": "P", "fil": "F",
	} {
		m.help.input.SetValue(q)
		m.help.filter()
		if got := helpTop(m).key; got != want {
			t.Errorf("%q: first %q, want %q", q, got, want)
		}
	}
}
