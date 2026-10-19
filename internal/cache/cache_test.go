package cache

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slack-T1.db")
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c.SaveUsers([]backend.User{{ID: "U1", Name: "anna"}})
	c.SaveConversations([]backend.Conversation{{ID: "C1", Name: "dev", LastRead: "1.0", Latest: "2.0", Cached: true}})
	c.SaveEmoji(map[string]string{"party": "https://x/party.gif"})
	c.SaveBots(map[string]string{"B1": "deploy"})
	var many []backend.Message
	for i := range MaxMessages + 50 {
		many = append(many, backend.Message{TS: fmt.Sprintf("%d.0", 100+i), User: "U1", Text: "hi"})
	}
	c.SaveList("C1", many, false)
	c.SaveList("C1/100.0", many, false) // a thread: parent + newest replies
	c.SaveList("C2", many[:2], false)
	c.DeleteList("C2")
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	c, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s, err := c.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Users) != 1 || s.Users[0].Name != "anna" {
		t.Errorf("users = %+v", s.Users)
	}
	if len(s.Conversations) != 1 || s.Conversations[0].LastRead != "1.0" || s.Conversations[0].Cached {
		t.Errorf("conversations = %+v (Cached must not be stored)", s.Conversations)
	}
	if s.Emoji["party"] == "" || s.Bots["B1"] != "deploy" {
		t.Errorf("emoji %v bots %v", s.Emoji, s.Bots)
	}
	l := s.Lists["C1"]
	if len(l.Msgs) != MaxMessages || l.Msgs[0].TS != "150.0" || !l.HasMore {
		t.Errorf("C1: %d msgs, first %s, more %v", len(l.Msgs), l.Msgs[0].TS, l.HasMore)
	}
	if _, ok := s.Lists["C1/100.0"]; ok {
		t.Error("threads should not be in the start snapshot")
	}
	if _, ok := s.Lists["C2"]; ok {
		t.Error("deleted list came back")
	}
	th, ok := c.LoadList("C1/100.0")
	if !ok || len(th.Msgs) != MaxMessages || th.Msgs[0].TS != "100.0" || th.Msgs[1].TS != "151.0" {
		t.Errorf("thread: ok=%v %d msgs", ok, len(th.Msgs))
	}
}
