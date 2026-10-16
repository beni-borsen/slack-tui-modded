package backend

import (
	"testing"

	"github.com/slack-go/slack"
)

func TestTSLess(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"", "1.000001", true},
		{"1712345678.000100", "1712345678.000200", true},
		{"1712345679.000001", "1712345678.999999", false},
		{"1712345678.1", "1712345678.1", false},
		{"999999999.000001", "1000000000.000001", true},
	} {
		if got := TSLess(tc.a, tc.b); got != tc.want {
			t.Errorf("TSLess(%q,%q)=%v", tc.a, tc.b, got)
		}
	}
}

func TestUnreadAndReply(t *testing.T) {
	c := Conversation{LastRead: "10.000001", Latest: "10.000002"}
	if !c.Unread() {
		t.Error("want unread")
	}
	c.LastRead = c.Latest
	if c.Unread() {
		t.Error("want read")
	}
	if !(Message{TS: "2", ThreadTS: "1"}).IsReply() || (Message{TS: "1", ThreadTS: "1"}).IsReply() ||
		(Message{TS: "2", ThreadTS: "1", SubType: "thread_broadcast"}).IsReply() {
		t.Error("IsReply wrong")
	}
}

func TestBotNameOverride(t *testing.T) {
	m := fromMsg(slack.Msg{BotID: "B1", Username: "Deploy v2", BotProfile: &slack.BotProfile{Name: "deploy-bot"}})
	if m.BotName != "Deploy v2" {
		t.Errorf("BotName = %q, want the per-message name", m.BotName)
	}
	m = fromMsg(slack.Msg{BotID: "B1", BotProfile: &slack.BotProfile{Name: "deploy-bot"}})
	if m.BotName != "deploy-bot" {
		t.Errorf("BotName = %q, want the profile name", m.BotName)
	}
}
