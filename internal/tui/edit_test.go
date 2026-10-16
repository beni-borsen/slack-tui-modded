package tui

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/kitty"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// chanWithMessages opens C2 with one message from Anna and one of ours,
// ours selected.
func chanWithMessages(t *testing.T) *Model {
	m := fakeModel(t, 110, 30)
	m.open("C2")
	m.update(historyMsg{id: "C2", msgs: []backend.Message{
		{TS: "100.000001", User: "U1", Text: "lunch?"},
		{TS: "100.000002", User: "ME", Text: "yes <@U1> &amp; <#C1>"},
	}})
	return m
}

func TestReactionToggle(t *testing.T) {
	m := chanWithMessages(t)
	m.handleKey(key("+"))
	if m.picker == nil {
		t.Fatal("+ did not open the picker")
	}
	for _, r := range "tada" {
		m.handleKey(key(string(r)))
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "🎉  :tada:") {
		t.Errorf("picker does not list tada:\n%s", v)
	}
	m.handleKey(key("enter"))
	msg := m.lists["C2"].msgs[1]
	if len(msg.Reactions) != 1 || msg.Reactions[0].Name != "tada" || msg.Reactions[0].Count != 1 {
		t.Fatalf("reaction not added: %+v", msg.Reactions)
	}
	// The live echo of our own reaction must not count twice.
	m.update(backend.EvReaction{Channel: "C2", TS: "100.000002", User: "ME", Name: "tada", Added: true})
	if c := m.lists["C2"].msgs[1].Reactions[0].Count; c != 1 {
		t.Errorf("echo double-counted: %d", c)
	}
	// Choosing it again removes it; a failed call puts it back.
	ev := backend.EvReaction{Channel: "C2", TS: "100.000002", User: "ME", Name: "tada"}
	m.toggleReaction("C2", "100.000002", "tada")
	if n := len(m.lists["C2"].msgs[1].Reactions); n != 0 {
		t.Errorf("toggle did not remove: %d", n)
	}
	m.update(reactedMsg{ev: ev, err: errors.New("no_reaction")})
	if n := len(m.lists["C2"].msgs[1].Reactions); n != 1 {
		t.Errorf("failed removal not undone: %d", n)
	}
}

func TestEditOwnMessage(t *testing.T) {
	m := chanWithMessages(t)
	m.handleKey(key("e"))
	if m.editing == nil {
		t.Fatal("e did not start editing")
	}
	if got := m.input.Value(); got != "yes @Anna Jensen & #dev" {
		t.Errorf("edit text = %q", got)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "editing · enter save · esc cancel") {
		t.Error("no editing label")
	}
	m.input.SetValue("yes @Anna Jensen & #dev, at 12 <3")
	m.handleKey(key("enter"))
	got := m.lists["C2"].msgs[1]
	if got.Text != "yes <@U1> &amp; <#C1>, at 12 &lt;3" || !got.Edited {
		t.Errorf("edited message = %q edited=%v", got.Text, got.Edited)
	}
	if m.insert || m.editing != nil {
		t.Error("still editing after save")
	}
	// Slack refusing the edit restores the old text.
	m.update(editedMsg{conv: "C2", old: backend.Message{TS: "100.000002", User: "ME", Text: "yes <@U1> &amp; <#C1>"}, err: errors.New("cant_update_message")})
	if got := m.lists["C2"].msgs[1].Text; got != "yes <@U1> &amp; <#C1>" {
		t.Errorf("not restored: %q", got)
	}
}

func TestEditRules(t *testing.T) {
	m := chanWithMessages(t)
	m.handleKey(key("k")) // select Anna's message
	m.handleKey(key("e"))
	if m.editing != nil || !strings.Contains(m.status, "only edit your own") {
		t.Errorf("edited someone else's message (status %q)", m.status)
	}
	// ↑ in an empty input edits our last message; esc cancels.
	m.handleKey(key("i"))
	m.handleKey(key("up"))
	if m.editing == nil || m.editing.msg.TS != "100.000002" {
		t.Fatal("↑ did not pick our last message")
	}
	m.handleKey(key("esc"))
	if m.editing != nil || m.input.Value() != "" || m.insert {
		t.Error("esc did not cancel the edit")
	}
}

func TestDeleteAsksFirst(t *testing.T) {
	m := chanWithMessages(t)
	m.handleKey(key("D"))
	if m.confirm == nil || !strings.Contains(ansi.Strip(m.View()), "delete this message? y/n") {
		t.Fatal("no confirmation")
	}
	m.handleKey(key("n"))
	if m.confirm != nil || len(m.lists["C2"].msgs) != 2 {
		t.Error("n did not cancel")
	}
	m.update(deletedMsg{conv: "C2", ts: "100.000002"})
	if len(m.lists["C2"].msgs) != 1 {
		t.Error("deleted message still shown")
	}
}

func TestCustomEmojiImage(t *testing.T) {
	m := chanWithMessages(t)
	f, _ := os.CreateTemp(t.TempDir(), "tty")
	m.SetGraphics(kitty.NewWriter(f, false), "")
	m.update(emojiMsg{emoji: map[string]string{"partyparrot": "https://emoji.slack-edge.com/T1/partyparrot/abc.gif", "pp": "alias:partyparrot"}})
	m.Update(backend.EvMessage{Channel: "C2", Msg: backend.Message{TS: "200.0", User: "U1", Text: "ship it :pp:"}})
	st := m.emojiImgs["partyparrot"]
	if st == nil {
		t.Fatal("alias :pp: did not request partyparrot's image")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "ship it :pp:") {
		t.Error("before loading, the chip should show")
	}
	m.update(emojiImgMsg{name: "partyparrot", png: []byte("x")})
	if n := strings.Count(m.View(), "\U0010EEEE"); n != 2 {
		t.Errorf("custom emoji drawn with %d cells, want 2", n)
	}
}

func TestCopyMessage(t *testing.T) {
	m := chanWithMessages(t)
	var buf strings.Builder
	m.clip = &buf
	m.handleKey(key("c"))
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("yes @Anna Jensen & #dev")) + "\a"
	if buf.String() != want {
		t.Errorf("wrote %q, want %q", buf.String(), want)
	}
	if !strings.Contains(m.status, "copied 23 characters") {
		t.Errorf("status %q", m.status)
	}
}

func TestSkinTonesAndMerging(t *testing.T) {
	m := chanWithMessages(t)
	m.lists["C2"].msgs[1].Reactions = []backend.Reaction{
		{Name: "pray", Count: 2, Users: []string{"U1", "U2"}},
		{Name: "pray::skin-tone-2", Count: 1, Users: []string{"ME"}},
		{Name: "+1::skin-tone-3", Count: 1, Users: []string{"U1"}},
	}
	v := ansi.Strip(m.View())
	if strings.Contains(v, "skin-tone") {
		t.Errorf("skin tone shown as text:\n%s", v)
	}
	if !strings.Contains(v, "🙏 3") || !strings.Contains(v, "👍 1") {
		t.Errorf("want merged 🙏 3 and 👍 1:\n%s", v)
	}
	// In message text too.
	m.update(backend.EvMessage{Channel: "C2", Msg: backend.Message{TS: "300.0", User: "U1", Text: "tak :pray::skin-tone-2:"}})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "tak 🙏") {
		t.Errorf("text skin tone not dropped:\n%s", v)
	}
}

func TestChangeEventKeepsReactions(t *testing.T) {
	m := chanWithMessages(t)
	m.lists["C2"].msgs[1].Reactions = []backend.Reaction{{Name: "firstresponder", Count: 1, Users: []string{"U1"}}}
	m.lists["C2"].msgs[1].Files = []backend.File{{ID: "F1", Name: "a.png"}}
	// A message_changed for the parent of a new thread reply, without reactions or files.
	m.update(backend.EvMessageChanged{Channel: "C2", Msg: backend.Message{TS: "100.000002", User: "ME", Text: "yes <@U1> &amp; <#C1>", ReplyCount: 2}})
	got := m.lists["C2"].msgs[1]
	if len(got.Reactions) != 1 || len(got.Files) != 1 || got.ReplyCount != 2 {
		t.Errorf("after change event: reactions=%v files=%v replies=%d", got.Reactions, got.Files, got.ReplyCount)
	}
}

func TestReopenRefreshes(t *testing.T) {
	m := chanWithMessages(t)
	// Locally the reaction went missing; reopening fetches the page again and the API's copy wins.
	m.open("C1")
	cmd := m.open("C2")
	if cmd == nil || !m.lists["C2"].loading {
		t.Fatal("reopening did not refresh")
	}
	m.update(historyMsg{id: "C2", msgs: []backend.Message{
		{TS: "100.000001", User: "U1", Text: "lunch?"},
		{TS: "100.000002", User: "ME", Text: "yes", Reactions: []backend.Reaction{{Name: "firstresponder", Count: 1, Users: []string{"U1"}}}},
	}})
	if r := m.lists["C2"].msgs[1].Reactions; len(r) != 1 {
		t.Errorf("refresh did not restore the reaction: %v", r)
	}
}

func TestMergeFresh(t *testing.T) {
	msg := func(ts string) backend.Message { return backend.Message{TS: ts} }
	local := []backend.Message{msg("1.0"), msg("2.0"), msg("5.0"), msg("9.0")} // 1,2 from scrolling up; 9 arrived live
	fresh := []backend.Message{msg("5.0"), msg("6.0"), msg("7.0")}
	out, more := mergeFresh(local, fresh, true, true)
	var got []string
	for _, x := range out {
		got = append(got, x.TS)
	}
	if strings.Join(got, ",") != "1.0,2.0,5.0,6.0,7.0,9.0" || !more {
		t.Errorf("merged %v more=%v", got, more)
	}
}

func tabKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyTab} }
