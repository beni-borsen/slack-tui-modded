package backend

import (
	"testing"

	"github.com/slack-go/slack"
)

func TestFromHit(t *testing.T) {
	h := fromHit(slack.SearchMessage{
		Timestamp: "250.000100", User: "U1", Text: "hi",
		Channel:   slack.CtxChannel{ID: "C1", Name: "dev"},
		Permalink: "https://acme.slack.com/archives/C1/p250000100?thread_ts=200.000100&cid=C1",
	})
	if h.ThreadTS != "200.000100" || !h.IsReply() || h.Channel != "C1" {
		t.Errorf("reply hit: %+v", h)
	}
	h = fromHit(slack.SearchMessage{Timestamp: "1.0", Username: "deploybot", Permalink: "https://acme.slack.com/archives/C1/p1"})
	if h.ThreadTS != "" || h.BotName != "deploybot" {
		t.Errorf("bot hit: %+v", h)
	}
}

func TestPermalink(t *testing.T) {
	c := &Client{TeamURL: "https://acme.slack.com/"}
	if got := c.Permalink("C1", "250.000100", ""); got != "https://acme.slack.com/archives/C1/p250000100" {
		t.Errorf("message: %s", got)
	}
	if got := c.Permalink("C1", "250.000100", "200.000100"); got != "https://acme.slack.com/archives/C1/p250000100?thread_ts=200.000100&cid=C1" {
		t.Errorf("reply: %s", got)
	}
}
