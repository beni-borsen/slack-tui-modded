package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/auth"
	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/cache"
	"github.com/magnusdalkvist/slack-tui/internal/config"
)

// TestCacheSession saves one session's state and starts the next from it.
func TestCacheSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slack-T1.db")

	m := fakeModel(t, 100, 24)
	c, err := cache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	m.SetCache(c)
	m.open("C1")
	m.Update(historyMsg{id: "C1", msgs: []backend.Message{
		{TS: "200.000001", User: "U1", Text: "old news"},
		{TS: "300.000001", User: "U1", Text: "cached hello", ThreadTS: "300.000001", ReplyCount: 1},
	}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // open the thread of the selected (newest) message
	m.Update(repliesMsg{id: "C1", ts: "300.000001", msgs: []backend.Message{
		{TS: "300.000001", User: "U1", Text: "cached hello", ThreadTS: "300.000001"},
		{TS: "301.000001", User: "U2", Text: "a reply", ThreadTS: "300.000001"},
	}})
	m.Flush()
	c.Close()

	// Next start: everything is drawn from the cache before the API answers.
	c, err = cache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cl := backend.New(auth.Tokens{})
	cl.SelfID, cl.TeamName = "ME", "Acme"
	m2 := New(context.Background(), cl, config.Default())
	m2.SetCache(c)
	m2.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m2.Update(m2.loadCache()())
	if !m2.booted || !m2.fromCache {
		t.Fatal("not started from the cache")
	}
	if !m2.convs["D1"].Unread() || m2.convs["C1"].Unread() {
		t.Error("cached unread markers lost")
	}
	m2.open("C1")
	if v := dump(t, m2); !strings.Contains(v, "cached hello") || !strings.Contains(v, "Anna Jensen") {
		t.Error("cached messages or names not shown")
	}
	m2.openThread(m2.lists["C1"].msgs[1])
	m2.Update(m2.loadThreadCache("C1", "300.000001")())
	if n := len(m2.thread.list.msgs); n != 2 {
		t.Errorf("cached thread has %d messages, want 2", n)
	}
	// The thread refetch drops a cached reply that was deleted since.
	m2.Update(repliesMsg{id: "C1", ts: "300.000001", msgs: []backend.Message{
		{TS: "300.000001", User: "U1", Text: "cached hello", ThreadTS: "300.000001"},
	}})
	if n := len(m2.thread.list.msgs); n != 1 {
		t.Errorf("deleted cached reply kept: %d messages", n)
	}

	// The API answers: a channel the user left is dropped, markers refresh.
	m2.Update(bootMsg{
		users: []backend.User{{ID: "U1", Name: "anna", RealName: "Anna Jensen"}},
		convs: []backend.Conversation{{ID: "C1", Name: "dev", Kind: backend.KindChannel}},
	})
	if m2.fromCache || len(m2.convs) != 1 {
		t.Errorf("fromCache=%v, %d conversations after sync", m2.fromCache, len(m2.convs))
	}
	m2.Update(infoMsg{conv: backend.Conversation{ID: "C1", InfoLoaded: true, LastRead: "300.000001", Latest: "300.000001"}})
	if m2.convs["C1"].Unread() {
		t.Error("fresh read marker did not replace the cached one")
	}
}
