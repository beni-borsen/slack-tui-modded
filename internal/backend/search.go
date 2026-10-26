package backend

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/slack-go/slack"
)

// Hit is one search.messages result.
type Hit struct {
	Message
	Channel     string // conversation ID
	ChannelName string // channel name; a user ID for DMs, mpdm-… for group DMs
	IsPrivate   bool
	IsGroup     bool
	Username    string
	Permalink   string
}

// HitStart and HitEnd wrap the matched words in a Hit's text.
const (
	HitStart = ""
	HitEnd   = ""
)

// SearchPage is one page of results, newest first.
type SearchPage struct {
	Hits  []Hit
	Total int
	Page  int
	Pages int
}

// Search runs Slack's message search (search.messages), so the query takes
// the same modifiers as in Slack: from:@anna in:#dev after:2026-01-01 "exact".
func (c *Client) Search(ctx context.Context, query string, page int) (SearchPage, error) {
	// By hand: slack-go's results leave out the files (images included).
	form := url.Values{
		"query": {query}, "sort": {"timestamp"}, "sort_dir": {"desc"}, "highlight": {"true"},
		"count": {"40"}, "page": {strconv.Itoa(page)},
	}
	var r struct {
		slack.SlackResponse
		Messages struct {
			Matches []searchMatch `json:"matches"`
			Total   int           `json:"total"`
			Paging  slack.Paging  `json:"paging"`
		} `json:"messages"`
	}
	if err := c.postForm(ctx, "search.messages", form, &r); err != nil {
		return SearchPage{}, fmt.Errorf("search.messages: %w", err)
	}
	out := SearchPage{Total: r.Messages.Total, Page: r.Messages.Paging.Page, Pages: r.Messages.Paging.Pages}
	for _, m := range r.Messages.Matches {
		out.Hits = append(out.Hits, fromHit(m))
	}
	return out, nil
}

type searchMatch struct {
	slack.SearchMessage
	Files []slack.File `json:"files"`
}

func fromHit(m searchMatch) Hit {
	msg := fromMsg(slack.Msg{Timestamp: m.Timestamp, User: m.User, Text: m.Text, Attachments: m.Attachments, Files: m.Files, Blocks: m.Blocks})
	// Results don't say whether they are thread replies; the permalink does.
	if u, err := url.Parse(m.Permalink); err == nil {
		msg.ThreadTS = u.Query().Get("thread_ts")
	}
	if msg.User == "" {
		msg.BotName = m.Username
	}
	return Hit{
		Message: msg, Channel: m.Channel.ID, ChannelName: m.Channel.Name,
		IsPrivate: m.Channel.IsPrivate, IsGroup: m.Channel.IsMPIM,
		Username: m.Username, Permalink: m.Permalink,
	}
}
