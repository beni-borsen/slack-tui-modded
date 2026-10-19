package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func TestPinToggle(t *testing.T) {
	m := fakeModel(t, 100, 24)
	m.open("C1")
	m.focus = fMessages
	m.Update(historyMsg{id: "C1", msgs: []backend.Message{
		{TS: "400.0", User: "U1", Text: "first"},
		{TS: "401.0", User: "U1", Text: "grouped under first"},
	}})
	m.lists["C1"].sel = 1
	m.Update(key("p"))
	if !m.lists["C1"].msgs[1].Pinned {
		t.Fatal("p should pin the selected message at once")
	}
	if v := dump(t, m); !strings.Contains(v, "grouped under first "+pinIcon+" pinned") {
		t.Errorf("a grouped pinned message should say so after its text")
	}
	// The call failed: back to unpinned.
	m.Update(pinnedMsg{conv: "C1", ts: "401.0", added: true, err: errors.New("pins.add: not_pinnable")})
	if m.lists["C1"].msgs[1].Pinned {
		t.Error("a failed pin should be undone")
	}
	// Someone else pins the first one.
	m.Update(backend.EvPin{Channel: "C1", TS: "400.0", Added: true})
	if !m.lists["C1"].msgs[0].Pinned {
		t.Error("pin_added not applied")
	}
	// An edit event without pinned_to keeps the pin.
	m.Update(backend.EvMessageChanged{Channel: "C1", Msg: backend.Message{TS: "400.0", User: "U1", Text: "first!", Edited: true}})
	if !m.lists["C1"].msgs[0].Pinned {
		t.Error("an edit dropped the pin")
	}
}

func TestPinsView(t *testing.T) {
	m := fakeModel(t, 100, 30)
	m.open("C1")
	m.Update(key("P"))
	if m.pins == nil || !m.pins.loading {
		t.Fatal("P should open the pins pane and load it")
	}
	for _, k := range []string{"p", "enter", "o", "G"} { // nothing to act on yet
		m.Update(key(k))
	}
	m.Update(pinsMsg{conv: "C1",
		bookmarks: []backend.Bookmark{{ID: "B1", Title: "Runbook", Link: "https://example.com/runbook"}},
		pins: []backend.Pin{
			{Message: backend.Message{TS: "250.0", ThreadTS: "200.0", User: "U1", Text: "deploy steps", Pinned: true}, Channel: "C1"},
			{Message: backend.Message{TS: "90.0", User: "U2", Text: "old news", Pinned: true}, Channel: "C1"},
		},
	})
	v := dump(t, m)
	for _, want := range []string{"Pinned & bookmarks", "# dev", "BOOKMARKS", "Runbook", "example.com/runbook", "PINNED", "Anna Jensen", "in thread", "old news"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	// p on the last pin unpins it and drops the row.
	m.Update(key("G"))
	m.Update(key("p"))
	if len(m.pins.pins) != 1 || m.pins.sel != 1 {
		t.Fatalf("unpin should drop the row: pins=%d sel=%d", len(m.pins.pins), m.pins.sel)
	}
	// enter on the thread reply goes there.
	m.Update(key("enter"))
	if m.pins != nil || m.jump == nil || m.jump.thread != "200.0" || m.thread == nil {
		t.Fatalf("enter should jump into the thread: pins=%v jump=%+v", m.pins, m.jump)
	}
}

func TestPinsNeedConversation(t *testing.T) {
	m := fakeModel(t, 100, 24)
	m.open(unreadsID)
	m.Update(key("P"))
	if m.pins != nil {
		t.Error("the Unreads view has no pins")
	}
}

func TestBookmarkEditing(t *testing.T) {
	m := fakeModel(t, 120, 30)
	m.open("C1")
	m.Update(key("P"))
	m.Update(pinsMsg{conv: "C1", bookmarks: []backend.Bookmark{{ID: "B1", Title: "Runbook", Link: "https://example.com/runbook"}}})

	m.Update(key("e"))
	if m.cmd == nil || m.cmd.input.Value() != "bookmark https://example.com/runbook Runbook" || m.cmd.bookmark == nil {
		t.Fatalf("e should open :bookmark with the bookmark: %+v", m.cmd)
	}
	m.Update(key(" "))
	m.Update(key("v2"))
	if m.pins == nil {
		t.Fatal("typing went to the pins pane, not the command line")
	}
	m.Update(key("enter"))
	if m.cmd != nil || m.bmEdit != nil {
		t.Error("the command line should close and forget the edit")
	}
	m.Update(bookmarkedMsg{conv: "C1", done: "bookmark changed"})
	if m.status != "bookmark changed" || !m.pins.loading {
		t.Errorf("status %q; an open list should be fetched again", m.status)
	}

	m.Update(pinsMsg{conv: "C1", bookmarks: []backend.Bookmark{{ID: "B1", Title: "Runbook v2", Link: "https://example.com/runbook"}}})
	m.Update(key("d"))
	if m.confirm == nil || m.confirm.prompt != "remove the bookmark Runbook v2? y/n" {
		t.Fatalf("confirm %+v", m.confirm)
	}
	m.Update(key("n"))
	if m.pins == nil || m.status != "cancelled" {
		t.Errorf("n should answer the question, not leave the pane: status %q", m.status)
	}

	m.Update(key("a"))
	if m.cmd == nil || m.cmd.input.Value() != "bookmark " || m.cmd.bookmark != nil {
		t.Fatalf("a should open an empty :bookmark: %+v", m.cmd)
	}
	m.Update(key("esc"))
}

func TestCmdBookmark(t *testing.T) {
	m := fakeModel(t, 100, 24)
	m.runCmd("bookmark example.com")
	if m.status != "open a conversation first" {
		t.Errorf("status %q", m.status)
	}
	m.open("C1")
	for line, want := range map[string]string{
		"bookmark":               "usage: :bookmark link [title]",
		"bookmark ftp://x.dk/f":  "not a web link: ftp://x.dk/f",
		"bookmark mailto:a@b.dk": "not a web link: https://mailto:a@b.dk",
	} {
		m.runCmd(line)
		if m.status != want {
			t.Errorf("%s: status %q, want %q", line, m.status, want)
		}
	}
	if m.runCmd("bookmark example.com/docs/ The docs") == nil {
		t.Error("a link should be added")
	}
}
