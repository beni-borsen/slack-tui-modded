package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func hit(conv, ts, thread, user, text string) backend.Hit {
	return backend.Hit{Message: backend.Message{TS: ts, ThreadTS: thread, User: user, Text: text}, Channel: conv, Permalink: "https://acme.slack.com/archives/" + conv}
}

func TestSearchView(t *testing.T) {
	m := fakeModel(t, 100, 24)
	m.Update(key("/"))
	m.Update(key("deploy"))
	m.Update(key("enter"))
	if !m.search.loading || m.search.typing {
		t.Fatalf("enter should start a search: %+v", m.search)
	}
	m.Update(searchMsg{seq: m.search.seq, query: "deploy", res: backend.SearchPage{Total: 2, Page: 1, Pages: 1, Hits: []backend.Hit{
		hit("C1", "300.000001", "", "U2", "deploying now, a long line that keeps going past the edge of the pane so it has to wrap a few times over"),
		hit("D1", "200.0", "150.0", "U1", "did the deploy work?"),
	}}})
	v := dump(t, m)
	for _, want := range []string{"2 results", "#dev", "Anna Jensen", "in thread", "deploying now"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	// A stale answer (an earlier query) is ignored.
	m.Update(searchMsg{seq: m.search.seq - 1, res: backend.SearchPage{Total: 9}})
	if m.search.total != 2 {
		t.Errorf("stale result applied")
	}
	// esc closes, / brings the results back.
	m.Update(key("esc"))
	if m.search.open {
		t.Fatal("esc should close the search")
	}
	m.Update(key("/"))
	if !m.search.open || len(m.search.hits) != 2 {
		t.Errorf("reopened search lost its results")
	}
}

func TestSearchJumpLoadsOlder(t *testing.T) {
	m := fakeModel(t, 100, 24)
	m.Update(key("/"))
	m.Update(key("x"))
	m.Update(key("enter"))
	m.Update(searchMsg{seq: m.search.seq, res: backend.SearchPage{Total: 1, Page: 1, Pages: 1, Hits: []backend.Hit{hit("C2", "50.0", "", "U1", "old")}}})
	m.Update(key("enter"))
	if m.current != "C2" || m.search.open || m.jump == nil {
		t.Fatalf("enter should open C2 and start a jump; current=%q jump=%v", m.current, m.jump)
	}
	// The newest page doesn't have it, so an older page is fetched.
	_, cmd := m.update(historyMsg{id: "C2", hasMore: true, msgs: []backend.Message{{TS: "100.0", User: "U2", Text: "new"}}})
	if cmd == nil || m.jump == nil || m.jump.pages != 1 {
		t.Fatalf("expected an older fetch, jump=%+v", m.jump)
	}
	m.Update(historyMsg{id: "C2", older: true, hasMore: true, msgs: []backend.Message{{TS: "40.0", User: "U2"}, {TS: "50.0", User: "U1", Text: "old"}}})
	if m.jump != nil {
		t.Fatalf("jump not resolved: %+v", m.jump)
	}
	if l := m.lists["C2"]; l.sel != 1 {
		t.Errorf("selected %d, want 1 (the hit)", l.sel)
	}
}

func TestSearchJumpThread(t *testing.T) {
	m := fakeModel(t, 120, 24)
	m.search = &searchState{open: true, query: "x", hits: []backend.Hit{hit("C1", "250.0", "200.0", "U1", "reply")}}
	m.Update(key("enter"))
	if m.thread == nil || m.thread.ts != "200.0" || m.focus != fThread {
		t.Fatalf("thread not opened: %+v", m.thread)
	}
	// The channel page arriving first must not end a thread jump.
	m.Update(historyMsg{id: "C1", msgs: []backend.Message{{TS: "200.0", User: "U2", ReplyCount: 2}}})
	if m.jump == nil {
		t.Fatal("channel history ended the thread jump")
	}
	m.Update(repliesMsg{id: "C1", ts: "200.0", msgs: []backend.Message{
		{TS: "200.0", User: "U2"}, {TS: "250.0", ThreadTS: "200.0", User: "U1", Text: "reply"}, {TS: "260.0", ThreadTS: "200.0", User: "ME"},
	}})
	if m.jump != nil || m.thread.list.sel != 1 {
		t.Errorf("reply not selected: sel=%d jump=%+v", m.thread.list.sel, m.jump)
	}
}

func TestSearchNotMember(t *testing.T) {
	m := fakeModel(t, 100, 24)
	m.search = &searchState{open: true, query: "x", hits: []backend.Hit{{Message: backend.Message{TS: "1.0"}, Channel: "C9", ChannelName: "elsewhere"}}}
	m.Update(key("enter"))
	if m.jump != nil || !strings.Contains(m.status, "not a member of #elsewhere") {
		t.Errorf("status %q jump %+v", m.status, m.jump)
	}
}

func TestUnmarkEntities(t *testing.T) {
	in := "a \ue000test\ue001 <https://\ue000test\ue001.dk|the \ue000test\ue001> b"
	want := "a \ue000test\ue001 <https://test.dk|the test> b"
	if got := unmarkEntities(in); got != want {
		t.Errorf("got %q", got)
	}
}

func TestSearchCompletion(t *testing.T) {
	m := fakeModel(t, 100, 24)
	m.Update(key("/"))
	m.Update(key("deploy from:@ann"))
	if m.search.complete == nil {
		t.Fatal("no suggestions for from:@ann")
	}
	v := dump(t, m)
	if !strings.Contains(v, "@anna") {
		t.Errorf("list lacks Anna's handle")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.search.input.Value(); got != "deploy from:@anna " {
		t.Errorf("after tab: %q", got)
	}
	// A bare name after in: suggests channels; enter picks rather than searches.
	m.Update(key("in:de"))
	m.Update(key("enter"))
	if got := m.search.input.Value(); got != "deploy from:@anna in:#dev " || m.search.loading {
		t.Errorf("after enter: %q loading=%v", got, m.search.loading)
	}
	// esc closes the list and keeps it closed for that word.
	m.Update(key("@bo"))
	m.Update(key("esc"))
	if m.search.complete != nil || !m.search.typing {
		t.Errorf("esc should close only the list")
	}
	m.Update(key("enter"))
	if !m.search.loading {
		t.Errorf("enter should now search")
	}
}
