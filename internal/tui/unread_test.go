package tui

import (
	"testing"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func TestTSBefore(t *testing.T) {
	for in, want := range map[string]string{"100.000002": "100.000001", "100.000000": "99.999999", "5.0": "4.999999"} {
		if got := backend.TSBefore(in); got != want {
			t.Errorf("TSBefore(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestMarkUnread(t *testing.T) {
	m := chanWithMessages(t)
	m.update(historyMsg{id: "C2", msgs: []backend.Message{
		{TS: "100.000001", User: "U1", Text: "lunch?"},
		{TS: "100.000002", User: "ME", Text: "yes"},
		{TS: "100.000003", User: "U1", Text: "12?"},
	}})
	c := m.convs["C2"]
	m.markRead("C2")
	m.update(key("k"))
	m.update(key("U"))
	if !c.Unread() || c.LastRead != "100.000001" {
		t.Fatalf("after U: unread=%v last_read=%s", c.Unread(), c.LastRead)
	}
	if l := m.lists["C2"]; l.sel != 1 || l.newSince != "100.000001" {
		t.Errorf("sel=%d newSince=%s; want the message kept selected under a new line", l.sel, l.newSince)
	}
	// A live message must not read it again while we sit on the message.
	m.update(backend.EvMessage{Channel: "C2", Msg: backend.Message{TS: "100.000004", User: "U1", Text: "?"}})
	if c.LastRead != "100.000001" {
		t.Errorf("a new message moved the marker to %s", c.LastRead)
	}
	m.update(key("G"))
	if c.Unread() {
		t.Error("reading to the bottom should mark it read again")
	}
}
