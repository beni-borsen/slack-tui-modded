package backend

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/slack-go/slack"
)

// PublicChannel is a channel anyone in the workspace can join.
type PublicChannel struct {
	ID, Name, Purpose string
	Members           int
	IsMember          bool
}

// PublicChannels lists the workspace's public channels that aren't
// archived, by name.
func (c *Client) PublicChannels(ctx context.Context) ([]PublicChannel, error) {
	var out []PublicChannel
	cursor := ""
	for {
		var chans []slack.Channel
		var next string
		err := retry(ctx, func() (err error) {
			chans, next, err = c.api.GetConversationsContext(ctx, &slack.GetConversationsParameters{
				Types: []string{"public_channel"}, ExcludeArchived: true, Limit: 1000, Cursor: cursor,
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("conversations.list: %w", err)
		}
		for _, ch := range chans {
			out = append(out, PublicChannel{
				ID: ch.ID, Name: ch.Name, Purpose: ch.Purpose.Value, Members: ch.NumMembers, IsMember: ch.IsMember,
			})
		}
		if next == "" {
			break
		}
		cursor = next
	}
	slices.SortFunc(out, func(a, b PublicChannel) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Join joins a public channel and returns it.
func (c *Client) Join(ctx context.Context, id string) (Conversation, error) {
	var ch *slack.Channel
	err := retry(ctx, func() (err error) {
		ch, _, _, err = c.api.JoinConversationContext(ctx, id)
		return err
	})
	if err != nil {
		return Conversation{}, fmt.Errorf("conversations.join: %w", err)
	}
	return fromChannel(*ch), nil
}

// Leave leaves a channel or group DM.
func (c *Client) Leave(ctx context.Context, id string) error {
	err := retry(ctx, func() error {
		_, err := c.api.LeaveConversationContext(ctx, id)
		return err
	})
	if err != nil {
		return fmt.Errorf("conversations.leave: %w", err)
	}
	return nil
}

// Create makes a channel, with us as its only member.
func (c *Client) Create(ctx context.Context, name string, private bool) (Conversation, error) {
	var ch *slack.Channel
	err := retry(ctx, func() (err error) {
		ch, err = c.api.CreateConversationContext(ctx, slack.CreateConversationParams{ChannelName: name, IsPrivate: private})
		return err
	})
	if err != nil {
		return Conversation{}, fmt.Errorf("conversations.create: %w", err)
	}
	return fromChannel(*ch), nil
}

// Invite adds people to a channel.
func (c *Client) Invite(ctx context.Context, id string, users []string) error {
	err := retry(ctx, func() error {
		_, err := c.api.InviteUsersToConversationContext(ctx, id, users...)
		return err
	})
	if err != nil {
		return fmt.Errorf("conversations.invite: %w", err)
	}
	return nil
}
