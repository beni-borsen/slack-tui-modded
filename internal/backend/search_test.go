package backend

import (
	"encoding/json"
	"testing"

	"github.com/slack-go/slack"
)

func TestFromHit(t *testing.T) {
	h := fromHit(searchMatch{SearchMessage: slack.SearchMessage{
		Timestamp: "250.000100", User: "U1", Text: "hi",
		Channel:   slack.CtxChannel{ID: "C1", Name: "dev"},
		Permalink: "https://acme.slack.com/archives/C1/p250000100?thread_ts=200.000100&cid=C1",
	}})
	if h.ThreadTS != "200.000100" || !h.IsReply() || h.Channel != "C1" {
		t.Errorf("reply hit: %+v", h)
	}
	h = fromHit(searchMatch{SearchMessage: slack.SearchMessage{Timestamp: "1.0", Username: "deploybot", Permalink: "https://acme.slack.com/archives/C1/p1"}})
	if h.ThreadTS != "" || h.BotName != "deploybot" {
		t.Errorf("bot hit: %+v", h)
	}
}

func TestSearchMatchFiles(t *testing.T) {
	var m searchMatch
	raw := `{"ts":"5.0","user":"U1","text":"","permalink":"https://acme.slack.com/archives/C1/p5","channel":{"id":"C1","name":"dev"},
		"files":[{"id":"F1","name":"image.png","mimetype":"image/png","url_private":"https://files.slack.com/F1","thumb_720":"https://files.slack.com/F1/720"}]}`
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	h := fromHit(m)
	if len(h.Files) != 1 || !h.Files[0].IsImage() || h.Channel != "C1" {
		t.Errorf("files lost: %+v", h)
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
