package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/render"
)

func typeText(m *Model, s string) {
	for _, r := range s {
		m.handleKey(key(string(r)))
	}
}

func TestEmojiCompletion(t *testing.T) {
	m := chanWithMessages(t)
	m.emoji = map[string]string{"partyparrot": "https://emoji.slack-edge.com/T/pp/a.gif"}
	m.handleKey(key("i"))
	typeText(m, "ship it :tad")
	if m.complete == nil || m.complete.items[0].emoji != "tada" {
		t.Fatalf("want tada first, got %+v", m.complete)
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "🎉  :tada:") || !strings.Contains(v, "tab insert") {
		t.Errorf("list not shown:\n%s", v)
	}
	m.handleKey(tabKey())
	if got := m.input.Value(); got != "ship it :tada: " {
		t.Errorf("after tab: %q", got)
	}
	if m.complete != nil {
		t.Error("list still open after inserting")
	}

	// Custom emoji are offered too; ↓ then enter picks the second match.
	typeText(m, "and :partyp")
	if m.complete == nil || m.complete.items[0].emoji != "partyparrot" {
		t.Fatalf("custom emoji not offered: %+v", m.complete)
	}
	m.handleKey(key("enter"))
	if got := m.input.Value(); got != "ship it :tada: and :partyparrot: " {
		t.Errorf("enter inserted %q", got)
	}
	// With the list closed, enter sends as usual.
	if !m.insert {
		t.Error("left insert mode")
	}
}

func TestCompletionRules(t *testing.T) {
	m := chanWithMessages(t)
	m.handleKey(key("i"))
	typeText(m, "meet at 12:30")
	if m.complete != nil {
		t.Error("12:30 opened the list")
	}
	m.input.Reset()
	typeText(m, ":t")
	if m.complete != nil {
		t.Error("one letter opened the list")
	}
	typeText(m, "h")
	if m.complete == nil {
		t.Fatal(":th did not open the list")
	}
	// esc closes the list but stays in insert mode, and the list stays
	// closed until the word changes.
	m.handleKey(key("esc"))
	if m.complete != nil || !m.insert {
		t.Errorf("esc: complete=%v insert=%v", m.complete != nil, m.insert)
	}
	typeText(m, "u")
	if m.complete == nil {
		t.Error("typing on did not reopen the list")
	}
}

func TestMentionCompletion(t *testing.T) {
	m := chanWithMessages(t)
	m.handleKey(key("i"))
	typeText(m, "hey @jens")
	if m.complete == nil || m.complete.items[0].label != "@Anna Jensen" {
		t.Fatalf("@jens should find Anna Jensen by her last name: %+v", m.complete)
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "@Anna Jensen  @anna") {
		t.Errorf("row should show the username:\n%s", v)
	}
	m.handleKey(tabKey())
	typeText(m, "see #de")
	if m.complete == nil || m.complete.items[0].label != "#dev" {
		t.Fatalf("#de should find #dev: %+v", m.complete)
	}
	m.handleKey(tabKey())
	typeText(m, "@he")
	if m.complete == nil || m.complete.items[0].insert != "@here" {
		t.Fatalf("@he should offer @here: %+v", m.complete)
	}
	m.handleKey(tabKey())
	if got := m.input.Value(); got != "hey @Anna Jensen see #dev @here " {
		t.Fatalf("input = %q", got)
	}
	// Sent as real mentions.
	got := render.Encode(strings.TrimSpace(m.input.Value()), m.composeTokens)
	if got != "hey <@U1> see <#C1> <!here>" {
		t.Errorf("encoded = %q", got)
	}
	// An e-mail address is not a mention.
	m.input.Reset()
	typeText(m, "mail anna@bo")
	if m.complete != nil {
		t.Error("an e-mail address opened the people list")
	}
}

func TestMentionWhileEditing(t *testing.T) {
	m := chanWithMessages(t)
	m.handleKey(key("e")) // our message "yes <@U1> & <#C1>"
	m.input.CursorEnd()
	typeText(m, " cc @bo")
	m.handleKey(tabKey())
	m.handleKey(key("enter"))
	if got := m.lists["C2"].msgs[1].Text; got != "yes <@U1> &amp; <#C1> cc <@U2>" {
		t.Errorf("edited text = %q", got)
	}
}
