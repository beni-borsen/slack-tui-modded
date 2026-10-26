package backend

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
)

// OpenConversation opens (or finds) the DM with one person, or the group
// DM with several, and returns it with its read marker.
func (c *Client) OpenConversation(ctx context.Context, users []string) (Conversation, error) {
	var ch *slack.Channel
	err := retry(ctx, func() (err error) {
		ch, _, _, err = c.api.OpenConversationContext(ctx, &slack.OpenConversationParameters{Users: users, ReturnIM: len(users) == 1})
		return err
	})
	if err != nil {
		return Conversation{}, fmt.Errorf("conversations.open: %w", err)
	}
	// conversations.open leaves out the group DM's name and the markers.
	return c.Info(ctx, ch.ID)
}

// Members lists the people in a conversation, by user ID.
func (c *Client) Members(ctx context.Context, id string) ([]string, error) {
	var out []string
	cursor := ""
	for {
		var ids []string
		var next string
		err := retry(ctx, func() (err error) {
			ids, next, err = c.api.GetUsersInConversationContext(ctx, &slack.GetUsersInConversationParameters{
				ChannelID: id, Limit: 1000, Cursor: cursor,
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("conversations.members: %w", err)
		}
		out = append(out, ids...)
		if next == "" {
			return out, nil
		}
		cursor = next
	}
}
