package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/auth"
	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/config"
)

// fakeModel builds a model with canned data and no network.
func fakeModel(t *testing.T, w, h int) *Model {
	t.Helper()
	// Anything the model saves (names cache, fold state, emoji, files) goes
	// to a temp dir, never the real ~/.cache or ~/.local/state.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	c := backend.New(auth.Tokens{})
	c.SelfID, c.TeamName = "ME", "Acme"
	cfg := config.Default()
	cfg.Sections = []config.Section{{Name: "Team", Items: []string{"#dev", "@anna"}}}
	m := New(context.Background(), c, cfg)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.Update(bootMsg{
		users: []backend.User{{ID: "ME", Name: "erik", DisplayName: "Erik"}, {ID: "U1", Name: "anna", RealName: "Anna Jensen"}, {ID: "U2", Name: "bob", DisplayName: "Bob"}},
		convs: []backend.Conversation{
			{ID: "C1", Name: "dev", Kind: backend.KindChannel, Topic: "Deploys and *builds*", LastRead: "100.000001", Latest: "300.000001"},
			{ID: "C2", Name: "general", Kind: backend.KindChannel, LastRead: "100.000001", Latest: "100.000001"},
			{ID: "C3", Name: "secret-project", Kind: backend.KindPrivate},
			{ID: "D1", Name: "U1", UserID: "U1", Kind: backend.KindDM, LastRead: "1.0", Latest: "200.0", Mentions: 2},
			{ID: "D2", Name: "U2", UserID: "U2", Kind: backend.KindDM},
			{ID: "G1", Name: "mpdm-erik--anna--bob-1", Kind: backend.KindGroup},
		},
	})
	return m
}

func dump(t *testing.T, m *Model) string {
	v := ansi.Strip(m.View())
	t.Log("\n" + v)
	for i, l := range strings.Split(v, "\n") {
		if w := ansi.StringWidth(l); w > m.width {
			t.Errorf("line %d is %d wide (> %d): %q", i, w, m.width, l)
		}
	}
	if n := strings.Count(v, "\n") + 1; n != m.height {
		t.Errorf("view has %d lines, want %d", n, m.height)
	}
	return v
}

func TestViewUnreads(t *testing.T) {
	m := fakeModel(t, 100, 24)
	m.open(unreadsID)
	m.Update(sinceMsg{id: "C1", msgs: []backend.Message{
		{TS: "200.000001", User: "U1", Text: "build is green <@ME> :tada:"},
		{TS: "300.000001", User: "U2", Text: "deploying now"},
	}})
	dump(t, m)
}

func TestViewChannelAndThread(t *testing.T) {
	m := fakeModel(t, 120, 26)
	m.open("C1")
	m.Update(historyMsg{id: "C1", msgs: []backend.Message{
		{TS: "100.000001", User: "U1", Text: "morning all"},
		{TS: "100.000002", User: "U1", Text: "second grouped message with a fairly long line that should wrap around the pane width nicely without breaking anything"},
		{TS: "200.000001", User: "U2", Text: "```go\nfmt.Println(1)\n```", ReplyCount: 2, LatestReply: "250.0", Reactions: []backend.Reaction{{Name: "+1", Count: 2, Users: []string{"ME", "U1"}}}},
		{TS: "300.000001", User: "U1", SubType: "channel_join", Text: "<@U1> has joined the channel"},
	}})
	dump(t, m)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(repliesMsg{id: "C1", ts: "200.000001", msgs: []backend.Message{
		{TS: "200.000001", User: "U2", Text: "```go\nfmt.Println(1)\n```"},
		{TS: "250.000000", User: "ME", ThreadTS: "200.000001", Text: "nice _one_"},
	}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	dump(t, m)
	if tgt, ts := m.inputTarget(); tgt != "C1" || ts != "200.000001" {
		t.Errorf("input target = %q %q", tgt, ts)
	}
}

func TestViewSwitcher(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("an")})
	dump(t, m)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.current != "D1" {
		t.Errorf("switcher opened %q, want D1 (Anna)", m.current)
	}
}

func TestLiveMessageFlow(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.open("C2")
	m.Update(historyMsg{id: "C2", msgs: []backend.Message{{TS: "100.000001", User: "U1", Text: "old"}}})
	// A message in another channel makes it unread and counts a mention.
	m.Update(backend.EvMessage{Channel: "C3", Msg: backend.Message{TS: "400.0", User: "U1", Text: "hey <@ME>"}})
	if c := m.convs["C3"]; !c.Unread() || c.Mentions != 1 {
		t.Errorf("C3 unread=%v mentions=%d", c.Unread(), c.Mentions)
	}
	// A message in the open channel appends.
	m.Update(backend.EvMessage{Channel: "C2", Msg: backend.Message{TS: "500.0", User: "U2", Text: "new"}})
	if n := len(m.lists["C2"].msgs); n != 2 {
		t.Errorf("C2 has %d msgs", n)
	}
	// Our own posted message then its echo must not duplicate.
	m.Update(postedMsg{id: "C2", ts: "600.0", text: "mine"})
	m.Update(backend.EvMessage{Channel: "C2", Msg: backend.Message{TS: "600.0", User: "ME", Text: "mine"}})
	if n := len(m.lists["C2"].msgs); n != 3 {
		t.Errorf("C2 has %d msgs after echo", n)
	}
	// Thread replies bump the parent once, even when posted then echoed.
	m.Update(postedMsg{id: "C2", threadTS: "500.0", ts: "700.0", text: "r"})
	m.Update(backend.EvMessage{Channel: "C2", Msg: backend.Message{TS: "700.0", ThreadTS: "500.0", User: "ME", Text: "r"}})
	if rc := m.lists["C2"].msgs[1].ReplyCount; rc != 1 {
		t.Errorf("reply count %d", rc)
	}
	m.Update(backend.EvMessageDeleted{Channel: "C2", TS: "100.000001"})
	m.Update(backend.EvReaction{Channel: "C2", TS: "500.0", User: "U1", Name: "eyes", Added: true})
	if l := m.lists["C2"]; len(l.msgs) != 2 || len(l.msgs[0].Reactions) != 1 {
		t.Errorf("after delete+reaction: %+v", l.msgs)
	}
	dump(t, m)
}

func TestSidebarFolding(t *testing.T) {
	m := fakeModel(t, 100, 30)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// Fold Channels: read channels hide, the unread #dev is in Team anyway.
	m.sideFindKey("section:Channels")
	m.toggleFold()
	var visible []string
	for _, it := range m.side {
		if it.id != "" {
			visible = append(visible, it.id)
		}
	}
	for _, id := range visible {
		if id == "C2" || id == "C3" {
			t.Errorf("folded Channels still shows %s", id)
		}
	}
	// The switcher still finds folded conversations.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("general")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.current != "C2" {
		t.Fatalf("switcher opened %q", m.current)
	}
	// Opening it shows it under the folded header.
	found := false
	for _, it := range m.side {
		found = found || it.id == "C2"
	}
	if !found {
		t.Error("open conversation hidden in folded section")
	}
	v := dump(t, m)
	if !strings.Contains(v, "▸ \uf4df CHANNELS 1") {
		t.Error("folded header should show hidden count")
	}
}

func TestSectionIcon(t *testing.T) {
	m := fakeModel(t, 110, 30)
	m.cfg.Sections[0].Icon = ":rocket:"
	if v := dump(t, m); !strings.Contains(v, "▾ 🚀 TEAM") {
		t.Error("section icon missing")
	}
	m.cfg.Sections[0].Icon = ":partyparrot:" // custom, not loaded: no icon, no :partyparrot: text
	if v := dump(t, m); !strings.Contains(v, "▾ TEAM") || strings.Contains(v, "partyparrot") {
		t.Error("an unloaded custom icon should leave the plain header")
	}
}

func TestBuiltinSectionIcons(t *testing.T) {
	m := fakeModel(t, 110, 30)
	v := dump(t, m)
	if !strings.Contains(v, "▾  CHANNELS") || !strings.Contains(v, "▾  DIRECT MESSAGES") {
		t.Error("built-in section icons missing")
	}
}
