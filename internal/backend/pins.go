package backend

import (
	"context"
	"fmt"
	"strings"

	"github.com/slack-go/slack"
)

// Pin is one pinned item: a message, or a file pinned on its own (then
// Message holds just the file and has no TS).
type Pin struct {
	Message
	Channel   string
	Permalink string
}

type Bookmark struct {
	ID, Title, Link, Emoji string
}

// Pins lists what is pinned in a conversation, newest pin first.
func (c *Client) Pins(ctx context.Context, id string) ([]Pin, error) {
	var items []slack.Item
	err := retry(ctx, func() (err error) {
		items, _, err = c.api.ListPinsContext(ctx, id)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("pins.list: %w", err)
	}
	var out []Pin
	for _, it := range items {
		switch {
		case it.Message != nil:
			p := Pin{Message: fromMsg(it.Message.Msg), Channel: id, Permalink: it.Message.Permalink}
			p.Pinned = true
			if p.Permalink == "" {
				p.Permalink = c.Permalink(id, p.TS, p.ThreadTS)
			}
			out = append(out, p)
		case it.File != nil:
			m := fromMsg(slack.Msg{Files: []slack.File{*it.File}})
			out = append(out, Pin{Message: m, Channel: id, Permalink: it.File.Permalink})
		}
	}
	return out, nil
}

// Bookmarks lists the links bookmarked at the top of a conversation.
func (c *Client) Bookmarks(ctx context.Context, id string) ([]Bookmark, error) {
	var bs []slack.Bookmark
	err := retry(ctx, func() (err error) {
		bs, err = c.api.ListBookmarksContext(ctx, id)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("bookmarks.list: %w", err)
	}
	out := make([]Bookmark, 0, len(bs))
	for _, b := range bs {
		out = append(out, Bookmark{ID: b.ID, Title: b.Title, Link: b.Link, Emoji: b.Emoji})
	}
	return out, nil
}

// Pin pins (or with add=false unpins) a message.
func (c *Client) Pin(ctx context.Context, id, ts string, add bool) error {
	ref := slack.NewRefToMessage(id, ts)
	method, call := "pins.remove", c.api.RemovePinContext
	if add {
		method, call = "pins.add", c.api.AddPinContext
	}
	if err := retry(ctx, func() error { return call(ctx, id, ref) }); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	return nil
}

// Permalink builds a message link the way Slack does, for when an answer
// doesn't carry one.
func (c *Client) Permalink(id, ts, threadTS string) string {
	u := strings.TrimSuffix(c.TeamURL, "/") + "/archives/" + id + "/p" + strings.Replace(ts, ".", "", 1)
	if threadTS != "" && threadTS != ts {
		u += "?thread_ts=" + threadTS + "&cid=" + id
	}
	return u
}
