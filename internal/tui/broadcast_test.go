package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func TestThreadReplyAlsoToChannel(t *testing.T) {
	m := fakeModel(t, 120, 26)
	m.open("C1")
	m.update(historyMsg{id: "C1", msgs: []backend.Message{{TS: "200.000001", User: "U2", Text: "deploy?", ReplyCount: 1}}})
	m.update(key("enter"))
	m.update(repliesMsg{id: "C1", ts: "200.000001", msgs: []backend.Message{{TS: "200.000001", User: "U2", Text: "deploy?"}}})
	m.update(key("i"))
	if v := dump(t, m); !strings.Contains(v, "ctrl+o also send to #dev") {
		t.Error("the thread composer should offer sending to the channel too")
	}
	m.update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if v := dump(t, m); !strings.Contains(v, "also sending to #dev") {
		t.Error("ctrl+o should turn it on")
	}
	m.update(key("done"))
	if cmd := m.update2(key("enter")); cmd == nil || m.broadcast {
		t.Error("enter should send and reset the choice")
	}
	m.update(postedMsg{id: "C1", threadTS: "200.000001", ts: "300.0", text: "done", broadcast: true})
	l := m.lists["C1"]
	if n := len(l.msgs); n != 2 || l.msgs[1].SubType != "thread_broadcast" {
		t.Errorf("the reply should show in the channel too; channel has %d messages", n)
	}
	if n := len(m.thread.list.msgs); n != 2 {
		t.Errorf("thread has %d messages, want 2", n)
	}

	// The channel's composer has no such choice.
	m.update(key("esc"))
	m.update(key("h"))
	m.update(key("i"))
	if v := dump(t, m); strings.Contains(v, "ctrl+o") {
		t.Error("only thread replies can go to the channel too")
	}
}

// update2 is update returning the command.
func (m *Model) update2(msg tea.Msg) tea.Cmd {
	_, cmd := m.update(msg)
	return cmd
}
