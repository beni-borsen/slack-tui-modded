package backend

import (
	"context"
	"io"
	"log"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// Events delivered from Socket Mode. They are plain values so the UI can
// handle them as Bubble Tea messages.

type EvConnection struct {
	State string // "connecting", "connected", "error"
	Err   error
}

// EvMessage is a new message, including thread replies and our own messages.
type EvMessage struct {
	Channel string
	Msg     Message
}

type EvMessageChanged struct {
	Channel string
	Msg     Message
}

type EvMessageDeleted struct {
	Channel  string
	TS       string
	ThreadTS string
}

type EvReaction struct {
	Channel string
	TS      string
	User    string
	Name    string
	Added   bool
}

// EvPin is a message pinned or unpinned, by anyone.
type EvPin struct {
	Channel string
	TS      string
	Added   bool
}

// EvConversationsChanged means our channel membership or a channel name
// changed; the UI should reload the conversation list.
type EvConversationsChanged struct{}

type EvUserChanged struct{ User User }

// EvDND is a change to the user's own Do Not Disturb, made here or elsewhere.
type EvDND struct{ DND DND }

// EvEmojiChanged means custom emoji were added, removed or renamed.
type EvEmojiChanged struct{}

// Listen runs Socket Mode until ctx is cancelled, sending events to out.
// Reconnects are handled by the library.
func (c *Client) Listen(ctx context.Context, out chan<- any) {
	sm := socketmode.New(c.api, socketmode.OptionLog(log.New(io.Discard, "", 0)))
	go func() {
		// RunContext always ends with an error; it only matters while we still want events.
		if err := sm.RunContext(ctx); ctx.Err() == nil {
			send(ctx, out, EvConnection{State: "error", Err: err})
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sm.Events:
			if !ok {
				return
			}
			switch ev.Type {
			case socketmode.EventTypeConnecting:
				send(ctx, out, EvConnection{State: "connecting"})
			case socketmode.EventTypeConnected:
				send(ctx, out, EvConnection{State: "connected"})
			case socketmode.EventTypeInvalidAuth:
				send(ctx, out, EvConnection{State: "error", Err: errInvalidAppToken})
			case socketmode.EventTypeConnectionError:
				send(ctx, out, EvConnection{State: "connecting"})
			case socketmode.EventTypeEventsAPI:
				if ev.Request != nil {
					sm.Ack(*ev.Request)
				}
				api, ok := ev.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				if e := c.translate(api.InnerEvent.Data); e != nil {
					send(ctx, out, e)
				}
			}
		}
	}
}

type constError string

func (e constError) Error() string { return string(e) }

const errInvalidAppToken = constError("Socket Mode rejected the app token — run `slack-tui login` again")

func send(ctx context.Context, out chan<- any, v any) {
	select {
	case out <- v:
	case <-ctx.Done():
	}
}

func (c *Client) translate(data any) any {
	switch e := data.(type) {
	case *slackevents.MessageEvent:
		return translateMessage(e)
	case *slackevents.ReactionAddedEvent:
		return EvReaction{Channel: e.Item.Channel, TS: e.Item.Timestamp, User: e.User, Name: e.Reaction, Added: true}
	case *slackevents.ReactionRemovedEvent:
		return EvReaction{Channel: e.Item.Channel, TS: e.Item.Timestamp, User: e.User, Name: e.Reaction}
	case *slackevents.PinAddedEvent:
		return pinEvent(e.Channel, e.Item, true)
	case *slackevents.PinRemovedEvent:
		return pinEvent(e.Channel, e.Item, false)
	case *slackevents.MemberJoinedChannelEvent:
		if e.User == c.SelfID {
			return EvConversationsChanged{}
		}
	case *slackevents.MemberLeftChannelEvent:
		if e.User == c.SelfID {
			return EvConversationsChanged{}
		}
	case *slackevents.ChannelRenameEvent, *slackevents.ChannelArchiveEvent, *slackevents.ChannelUnarchiveEvent:
		return EvConversationsChanged{}
	case *slackevents.EmojiChangedEvent:
		return EvEmojiChanged{}
	case *slackevents.UserChangeEvent:
		u := e.User
		real := u.RealName
		if real == "" {
			real = u.Profile.RealName
		}
		return EvUserChanged{User: User{
			ID: u.ID, Name: u.Name, DisplayName: u.Profile.DisplayName, RealName: real,
			Deleted: u.Deleted, IsBot: u.IsBot, TZ: u.TZ, TeamID: u.TeamID,
			StatusEmoji: u.Profile.StatusEmoji, StatusText: u.Profile.StatusText, StatusExpires: int64(u.Profile.StatusExpiration),
		}}
	case *slackevents.DndUpdatedEvent:
		if e.User != c.SelfID {
			return nil
		}
		s := e.DndStatus
		return EvDND{DND: newDND(s.DndEnabled, s.NextDndStartTs, s.NextDndEndTs, s.SnoozeEnabled, s.SnoozeEndtime)}
	}
	return nil
}

func pinEvent(channel string, it slackevents.Item, added bool) any {
	if channel == "" {
		channel = it.Channel
	}
	ts := it.Timestamp
	if it.Message != nil && it.Message.Timestamp != "" {
		ts = it.Message.Timestamp
	}
	// TS is empty for a file pinned on its own.
	return EvPin{Channel: channel, TS: ts, Added: added}
}

func translateMessage(e *slackevents.MessageEvent) any {
	switch e.SubType {
	case "message_changed":
		if e.Message == nil {
			return nil
		}
		return EvMessageChanged{Channel: e.Channel, Msg: fromMsg(*e.Message)}
	case "message_deleted":
		ev := EvMessageDeleted{Channel: e.Channel, TS: e.DeletedTimeStamp}
		if e.PreviousMessage != nil {
			ev.ThreadTS = e.PreviousMessage.ThreadTimestamp
			if ev.TS == "" {
				ev.TS = e.PreviousMessage.Timestamp
			}
		}
		return ev
	case "message_replied":
		// Deprecated parent update; replies arrive as their own events.
		return nil
	}
	var m slack.Msg
	if e.Message != nil {
		m = *e.Message
	} else {
		m = slack.Msg{Timestamp: e.TimeStamp, ThreadTimestamp: e.ThreadTimeStamp, User: e.User, Text: e.Text, SubType: e.SubType, Username: e.Username, BotID: e.BotID, Blocks: e.Blocks}
	}
	if m.Timestamp == "" {
		m.Timestamp = e.TimeStamp
	}
	if m.Channel == "" {
		m.Channel = e.Channel
	}
	return EvMessage{Channel: e.Channel, Msg: fromMsg(m)}
}
