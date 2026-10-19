package backend

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/slack-go/slack"

	"github.com/magnusdalkvist/slack-tui/internal/auth"
)

type Client struct {
	api   *slack.Client
	token string // user token, for file downloads

	SelfID   string
	TeamID   string
	TeamName string
	TeamURL  string
}

func New(t auth.Tokens) *Client {
	return &Client{api: slack.New(t.User, slack.OptionAppLevelToken(t.App)), token: t.User}
}

// Auth checks the user token and records who we are.
func (c *Client) Auth(ctx context.Context) error {
	r, err := c.api.AuthTestContext(ctx)
	if err != nil {
		return fmt.Errorf("auth.test: %w", err)
	}
	c.SelfID, c.TeamID, c.TeamName, c.TeamURL = r.UserID, r.TeamID, r.Team, r.URL
	return nil
}

// retry runs fn, sleeping and retrying when Slack rate-limits us.
func retry(ctx context.Context, fn func() error) error {
	for attempt := 0; ; attempt++ {
		err := fn()
		var rl *slack.RateLimitedError
		if !errors.As(err, &rl) || attempt >= 5 {
			return err
		}
		wait := rl.RetryAfter
		if wait <= 0 {
			wait = time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (c *Client) Users(ctx context.Context) ([]User, error) {
	var us []slack.User
	err := retry(ctx, func() (err error) {
		us, err = c.api.GetUsersContext(ctx, slack.GetUsersOptionLimit(200))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("users.list: %w", err)
	}
	out := make([]User, 0, len(us))
	for _, u := range us {
		out = append(out, fromUser(u))
	}
	return out, nil
}

// Conversations lists every conversation the user is a member of.
func (c *Client) Conversations(ctx context.Context) ([]Conversation, error) {
	var out []Conversation
	cursor := ""
	for {
		var chans []slack.Channel
		var next string
		err := retry(ctx, func() (err error) {
			chans, next, err = c.api.GetConversationsForUserContext(ctx, &slack.GetConversationsForUserParameters{
				Types:           []string{"public_channel", "private_channel", "im", "mpim"},
				Limit:           200,
				ExcludeArchived: true,
				Cursor:          cursor,
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("users.conversations: %w", err)
		}
		for _, ch := range chans {
			out = append(out, fromChannel(ch))
		}
		if next == "" {
			return out, nil
		}
		cursor = next
	}
}

func fromChannel(ch slack.Channel) Conversation {
	c := Conversation{ID: ch.ID, Name: ch.Name, Topic: ch.Topic.Value, LastRead: ch.LastRead, External: ch.IsExtShared}
	switch {
	case ch.IsIM:
		c.Kind, c.UserID, c.Name = KindDM, ch.User, ch.User
	case ch.IsMpIM:
		c.Kind = KindGroup
	case ch.IsPrivate:
		c.Kind = KindPrivate
	default:
		c.Kind = KindChannel
	}
	if ch.Latest != nil {
		c.Latest = ch.Latest.Timestamp
	}
	return c
}

// Info fetches the read marker and newest message for one conversation.
// conversations.info does not always include the newest message, so it
// falls back to a one-message history call.
func (c *Client) Info(ctx context.Context, id string) (Conversation, error) {
	var ch *slack.Channel
	err := retry(ctx, func() (err error) {
		ch, err = c.api.GetConversationInfoContext(ctx, &slack.GetConversationInfoInput{ChannelID: id})
		return err
	})
	if err != nil {
		return Conversation{}, fmt.Errorf("conversations.info %s: %w", id, err)
	}
	conv := fromChannel(*ch)
	conv.InfoLoaded = true
	if conv.IsDM() {
		conv.Mentions = ch.UnreadCountDisplay
	}
	if conv.Latest == "" {
		msgs, _, err := c.History(ctx, id, "", 1)
		if err != nil {
			return conv, err
		}
		if len(msgs) > 0 {
			conv.Latest = msgs[len(msgs)-1].TS
		}
	}
	return conv, nil
}

// History returns up to limit messages older than before (or the newest
// ones when before is empty), oldest first, and whether more exist.
func (c *Client) History(ctx context.Context, id, before string, limit int) ([]Message, bool, error) {
	var r *slack.GetConversationHistoryResponse
	err := retry(ctx, func() (err error) {
		r, err = c.api.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
			ChannelID: id, Latest: before, Limit: limit,
		})
		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("conversations.history: %w", err)
	}
	out := make([]Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		out = append(out, fromMsg(m.Msg))
	}
	slices.Reverse(out)
	return out, r.HasMore, nil
}

// Since returns messages newer than after, oldest first.
func (c *Client) Since(ctx context.Context, id, after string, limit int) ([]Message, error) {
	var r *slack.GetConversationHistoryResponse
	err := retry(ctx, func() (err error) {
		r, err = c.api.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
			ChannelID: id, Oldest: after, Limit: limit,
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("conversations.history: %w", err)
	}
	out := make([]Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		out = append(out, fromMsg(m.Msg))
	}
	slices.Reverse(out)
	return out, nil
}

// Replies returns a thread: the parent first, then the replies.
func (c *Client) Replies(ctx context.Context, id, threadTS string) ([]Message, error) {
	var out []Message
	cursor := ""
	for {
		var msgs []slack.Message
		var next string
		err := retry(ctx, func() (err error) {
			msgs, _, next, err = c.api.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{
				ChannelID: id, Timestamp: threadTS, Limit: 200, Cursor: cursor,
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("conversations.replies: %w", err)
		}
		for _, m := range msgs {
			out = append(out, fromMsg(m.Msg))
		}
		if next == "" {
			return out, nil
		}
		cursor = next
	}
}

// Post sends a message as the user, into a thread when threadTS is set.
func (c *Client) Post(ctx context.Context, id, text, threadTS string) (string, error) {
	opts := []slack.MsgOption{slack.MsgOptionLinkNames(true)}
	if blocks := listBlocks(text); blocks != nil {
		opts = append(opts, slack.MsgOptionText(listFallback(text), false), slack.MsgOptionBlocks(blocks...))
	} else {
		opts = append(opts, slack.MsgOptionText(text, false))
	}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}
	var ts string
	err := retry(ctx, func() (err error) {
		_, ts, err = c.api.PostMessageContext(ctx, id, opts...)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("chat.postMessage: %w", err)
	}
	return ts, nil
}

// Mark moves the read marker, so Slack on other devices agrees.
func (c *Client) Mark(ctx context.Context, id, ts string) error {
	return retry(ctx, func() error { return c.api.MarkConversationContext(ctx, id, ts) })
}

// CustomEmoji returns the workspace's custom emoji: name → image URL, or
// "alias:<other>" for aliases.
func (c *Client) CustomEmoji(ctx context.Context) (map[string]string, error) {
	var out map[string]string
	err := retry(ctx, func() (err error) {
		out, err = c.api.GetEmojiContext(ctx)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("emoji.list: %w", err)
	}
	return out, nil
}

// LatestImage returns the newest image file the user can see, for doctor.
func (c *Client) LatestImage(ctx context.Context) (*File, error) {
	files, _, err := c.api.GetFilesContext(ctx, slack.GetFilesParameters{Types: "images", Count: 1})
	if err != nil {
		return nil, fmt.Errorf("files.list: %w", err)
	}
	if len(files) == 0 {
		return nil, nil
	}
	m := fromMsg(slack.Msg{Files: files[:1]})
	return &m.Files[0], nil
}

// React adds (or with add=false removes) an emoji reaction to a message.
func (c *Client) React(ctx context.Context, id, ts, name string, add bool) error {
	ref := slack.NewRefToMessage(id, ts)
	return retry(ctx, func() error {
		if add {
			return c.api.AddReactionContext(ctx, name, ref)
		}
		return c.api.RemoveReactionContext(ctx, name, ref)
	})
}

// Edit replaces the text of one of the user's own messages.
func (c *Client) Edit(ctx context.Context, id, ts, text string) error {
	err := retry(ctx, func() error {
		// No list: an empty block list clears one the message had.
		opt := []slack.MsgOption{slack.MsgOptionText(text, false), slack.MsgOptionBlocks()}
		if blocks := listBlocks(text); blocks != nil {
			opt = []slack.MsgOption{slack.MsgOptionText(listFallback(text), false), slack.MsgOptionBlocks(blocks...)}
		}
		_, _, _, err := c.api.UpdateMessageContext(ctx, id, ts, opt...)
		return err
	})
	if err != nil {
		return fmt.Errorf("chat.update: %w", err)
	}
	return nil
}

// Delete removes one of the user's own messages.
func (c *Client) Delete(ctx context.Context, id, ts string) error {
	err := retry(ctx, func() error {
		_, _, err := c.api.DeleteMessageContext(ctx, id, ts)
		return err
	})
	if err != nil {
		return fmt.Errorf("chat.delete: %w", err)
	}
	return nil
}

// OpenDM returns the direct-message conversation with a user, opening it if
// needed.
func (c *Client) OpenDM(ctx context.Context, userID string) (string, error) {
	var ch *slack.Channel
	err := retry(ctx, func() (err error) {
		ch, _, _, err = c.api.OpenConversationContext(ctx, &slack.OpenConversationParameters{Users: []string{userID}, ReturnIM: true})
		return err
	})
	if err != nil {
		return "", fmt.Errorf("conversations.open: %w", err)
	}
	return ch.ID, nil
}

// BotName looks up a bot's name, for messages that carry only its ID.
func (c *Client) BotName(ctx context.Context, id string) (string, error) {
	var b *slack.Bot
	err := retry(ctx, func() (err error) {
		b, err = c.api.GetBotInfoContext(ctx, slack.GetBotInfoParameters{Bot: id})
		return err
	})
	if err != nil {
		return "", fmt.Errorf("bots.info: %w", err)
	}
	return b.Name, nil
}

// UserInfo looks up one person, for those users.list leaves out: people
// from other organisations in shared (Slack Connect) channels.
func (c *Client) UserInfo(ctx context.Context, id string) (User, error) {
	var u *slack.User
	err := retry(ctx, func() (err error) {
		u, err = c.api.GetUserInfoContext(ctx, id)
		return err
	})
	if err != nil {
		return User{}, fmt.Errorf("users.info: %w", err)
	}
	return fromUser(*u), nil
}

func fromUser(u slack.User) User {
	real := u.RealName
	if real == "" {
		real = u.Profile.RealName // external people often only have it here
	}
	return User{
		ID: u.ID, Name: u.Name, DisplayName: u.Profile.DisplayName, RealName: real,
		Deleted: u.Deleted, IsBot: u.IsBot, TZ: u.TZ, TeamID: u.TeamID,
		StatusEmoji: u.Profile.StatusEmoji, StatusText: u.Profile.StatusText, StatusExpires: int64(u.Profile.StatusExpiration),
	}
}
