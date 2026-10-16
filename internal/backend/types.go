// Package backend talks to Slack: the Web API for reads and writes, Socket
// Mode for live events. It knows nothing about the UI.
package backend

import (
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

type Kind int

const (
	KindChannel Kind = iota // public channel
	KindPrivate             // private channel
	KindDM                  // 1:1 direct message
	KindGroup               // group DM (mpim)
)

type Conversation struct {
	ID       string
	Name     string // channel name; for DMs the peer's user ID; for group DMs the mpdm-… name
	Kind     Kind
	UserID   string // DM peer
	Topic    string
	LastRead string // ts of the last message the user has read
	Latest   string // ts of the newest top-level message we know of
	// Mentions is Slack's unread_count_display for DMs, and our own count of
	// live @-mentions for channels.
	Mentions int
	// InfoLoaded is set once LastRead/Latest came from conversations.info.
	InfoLoaded bool
	// External is a Slack Connect conversation, shared with another organisation.
	External bool
}

// Unread reports whether there are messages newer than the read marker.
func (c *Conversation) Unread() bool {
	return c.Latest != "" && TSLess(c.LastRead, c.Latest)
}

func (c *Conversation) IsDM() bool { return c.Kind == KindDM || c.Kind == KindGroup }

type User struct {
	ID          string
	Name        string // username
	DisplayName string
	RealName    string
	Deleted     bool
	IsBot       bool
	TZ          string
	TeamID      string // the person's home workspace
}

// Label is the name Slack shows for the user.
func (u User) Label() string {
	switch {
	case u.DisplayName != "":
		return u.DisplayName
	case u.RealName != "":
		return u.RealName
	default:
		return u.Name
	}
}

type Reaction struct {
	Name  string
	Count int
	Users []string
}

type File struct {
	ID, Name, Title, Mimetype, URL, Permalink string
	// Thumb is the best thumbnail Slack made (images only); W/H are the
	// original's pixel size.
	Thumb string
	W, H  int
}

// IsImage is true for files Slack made a picture thumbnail of.
func (f File) IsImage() bool {
	return strings.HasPrefix(f.Mimetype, "image/") && (f.Thumb != "" || f.URL != "")
}

type Message struct {
	TS          string
	ThreadTS    string
	User        string
	BotName     string
	BotID       string // for bots that send neither a profile nor a username
	Text        string
	SubType     string
	ReplyCount  int
	LatestReply string
	Edited      bool
	Reactions   []Reaction
	Files       []File
}

// IsReply is true for thread replies that are not also broadcast to the channel.
func (m Message) IsReply() bool {
	return m.ThreadTS != "" && m.ThreadTS != m.TS && m.SubType != "thread_broadcast"
}

func (m Message) Time() time.Time { return TSTime(m.TS) }

func fromMsg(m slack.Msg) Message {
	out := Message{
		TS:          m.Timestamp,
		ThreadTS:    m.ThreadTimestamp,
		User:        m.User,
		BotID:       m.BotID,
		Text:        m.Text,
		SubType:     m.SubType,
		ReplyCount:  m.ReplyCount,
		LatestReply: m.LatestReply,
		Edited:      m.Edited != nil,
	}
	// A name the bot gives itself per message ("Deploy v2") wins over its
	// profile's name ("deploy-bot"), as in Slack.
	if m.Username != "" {
		out.BotName = m.Username
	} else if out.User == "" && m.BotProfile != nil {
		out.BotName = m.BotProfile.Name
	}
	// Bot messages often carry everything in attachments.
	var extra []string
	for _, a := range m.Attachments {
		switch {
		case a.Text != "":
			extra = append(extra, a.Text)
		case a.Fallback != "":
			extra = append(extra, a.Fallback)
		case a.Title != "":
			extra = append(extra, a.Title)
		}
	}
	if len(extra) > 0 {
		if out.Text != "" {
			out.Text += "\n"
		}
		out.Text += strings.Join(extra, "\n")
	}
	for _, r := range m.Reactions {
		out.Reactions = append(out.Reactions, Reaction{Name: r.Name, Count: r.Count, Users: r.Users})
	}
	for _, f := range m.Files {
		thumb := f.Thumb720
		if thumb == "" {
			thumb = f.Thumb480
		}
		if thumb == "" {
			thumb = f.Thumb360
		}
		out.Files = append(out.Files, File{
			ID: f.ID, Name: f.Name, Title: f.Title, Mimetype: f.Mimetype, URL: f.URLPrivate, Permalink: f.Permalink,
			Thumb: thumb, W: f.OriginalW, H: f.OriginalH,
		})
	}
	return out
}

// TSLess compares two Slack timestamps ("1712345678.123456"). An empty
// timestamp sorts first.
func TSLess(a, b string) bool {
	as, au := splitTS(a)
	bs, bu := splitTS(b)
	if as != bs {
		return as < bs
	}
	return au < bu
}

func splitTS(ts string) (int64, int64) {
	sec, micro, _ := strings.Cut(ts, ".")
	s, _ := strconv.ParseInt(sec, 10, 64)
	u, _ := strconv.ParseInt(micro, 10, 64)
	return s, u
}

func TSTime(ts string) time.Time {
	s, u := splitTS(ts)
	return time.Unix(s, u*1000)
}
