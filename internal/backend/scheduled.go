package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// Scheduled is a message waiting to be sent at PostAt. Slack's list doesn't
// say whether it goes into a thread.
type Scheduled struct {
	ID      string
	Channel string
	Text    string
	PostAt  time.Time
}

// Schedule queues a message to be sent as the user at a later time, into a
// thread when threadTS is set.
func (c *Client) Schedule(ctx context.Context, id, text, threadTS string, at time.Time) (Scheduled, error) {
	opts := []slack.MsgOption{slack.MsgOptionText(text, false), slack.MsgOptionLinkNames(true)}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}
	var sid string
	err := retry(ctx, func() (err error) {
		_, sid, err = c.api.ScheduleMessageContext(ctx, id, strconv.FormatInt(at.Unix(), 10), opts...)
		return err
	})
	if err != nil {
		return Scheduled{}, fmt.Errorf("chat.scheduleMessage: %w", err)
	}
	return Scheduled{ID: sid, Channel: id, Text: text, PostAt: at.Truncate(time.Second)}, nil
}

// ScheduledMessages lists every message the user has scheduled, soonest
// first.
func (c *Client) ScheduledMessages(ctx context.Context) ([]Scheduled, error) {
	var out []Scheduled
	cursor := ""
	for {
		var page []slack.ScheduledMessage
		var next string
		err := retry(ctx, func() (err error) {
			page, next, err = c.api.GetScheduledMessagesContext(ctx, &slack.GetScheduledMessagesParameters{Cursor: cursor, Limit: 100})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("chat.scheduledMessages.list: %w", err)
		}
		for _, s := range page {
			out = append(out, Scheduled{ID: s.ID, Channel: s.Channel, Text: s.Text, PostAt: time.Unix(int64(s.PostAt), 0)})
		}
		if next == "" {
			break
		}
		cursor = next
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PostAt.Before(out[j].PostAt) })
	return out, nil
}

// Unschedule cancels a scheduled message. It is posted by hand: slack-go
// always adds as_user=false, which Slack now refuses ("invalid_arguments",
// deprecated_argument as_user).
func (c *Client) Unschedule(ctx context.Context, channel, id string) error {
	form := url.Values{"channel": {channel}, "scheduled_message_id": {id}}
	if err := c.postForm(ctx, "chat.deleteScheduledMessage", form, &slack.SlackResponse{}); err != nil {
		return fmt.Errorf("chat.deleteScheduledMessage: %w", err)
	}
	return nil
}

// apiResult is an answer from the Web API; Err reports ok=false.
type apiResult interface{ Err() error }

// postForm calls a Web API method by hand, for where slack-go sends
// arguments Slack refuses or drops fields we need. out is decoded from the
// answer.
func (c *Client) postForm(ctx context.Context, method string, form url.Values, out apiResult) error {
	return retry(ctx, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, slack.APIURL+method, strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			return &slack.RateLimitedError{RetryAfter: time.Duration(wait) * time.Second}
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("HTTP %d: %w", resp.StatusCode, err)
		}
		return out.Err()
	})
}
