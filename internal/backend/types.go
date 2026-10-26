// Package backend talks to Slack: the Web API for reads and writes, Socket
// Mode for live events. It knows nothing about the UI.
package backend

import (
	"fmt"
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
	// Members is how many people are in it, once conversations.info told us.
	Members int
	// Cached is set while the markers come from the last session's cache.
	Cached bool `json:"-"`
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
	// Custom status; StatusExpires is unix seconds, 0 for "don't clear".
	StatusEmoji   string
	StatusText    string
	StatusExpires int64
	// For the profile card. Email needs the users:read.email scope.
	Title, Pronouns, Email, Phone string
	TZLabel                       string // "Central European Summer Time"
	TZOffset                      int    // seconds east of UTC, now
}

// Status is the person's custom status, or empty strings once it has
// expired (Slack clears it a little after that).
func (u User) Status(now time.Time) (emoji, text string) {
	if u.StatusExpires > 0 && now.Unix() >= u.StatusExpires {
		return "", ""
	}
	return u.StatusEmoji, u.StatusText
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
	// Anim is an animated version for GIFs (thumbnails are still frames).
	Anim string
	W, H int
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
	Pinned      bool // pinned in its conversation
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
		Pinned:      len(m.PinnedTo) > 0,
	}
	// A name the bot gives itself per message ("Deploy v2") wins over its
	// profile's name ("deploy-bot"), as in Slack.
	if m.Username != "" {
		out.BotName = m.Username
	} else if out.User == "" && m.BotProfile != nil {
		out.BotName = m.BotProfile.Name
	}
	// Slack flattens the text of a message with a list onto one line;
	// its blocks still have the lines.
	if t, ok := blocksText(m.Blocks); ok {
		out.Text = t
	}
	// Bot messages often carry everything in attachments.
	var extra []string
	for _, a := range m.Attachments {
		extra = append(extra, attachmentText(a)...)
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
		out.Files = append(out.Files, fromFile(f))
	}
	return out
}

// attachmentText is what Slack shows of an attachment: the pretext, the
// title (a link when it has one) and the text, or the fallback when there is
// neither title nor text.
func attachmentText(a slack.Attachment) []string {
	var out []string
	if a.Pretext != "" {
		out = append(out, a.Pretext)
	}
	switch {
	case a.Title != "" && a.TitleLink != "":
		title := strings.NewReplacer("|", "¦", ">", "›").Replace(a.Title) // must not end the link early
		out = append(out, "<"+a.TitleLink+"|"+title+">")
	case a.Title != "":
		out = append(out, a.Title)
	}
	if a.Text != "" {
		out = append(out, a.Text)
	} else if a.Title == "" && a.Fallback != "" {
		out = append(out, a.Fallback)
	}
	return out
}

func fromFile(f slack.File) File {
	thumb := f.Thumb720
	if thumb == "" {
		thumb = f.Thumb480
	}
	if thumb == "" {
		thumb = f.Thumb360
	}
	anim := f.Thumb360Gif
	if anim == "" && f.Mimetype == "image/gif" {
		anim = f.URLPrivate
	}
	return File{
		ID: f.ID, Name: f.Name, Title: f.Title, Mimetype: f.Mimetype, URL: f.URLPrivate, Permalink: f.Permalink,
		Thumb: thumb, Anim: anim, W: f.OriginalW, H: f.OriginalH,
	}
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

// TSBefore is the timestamp one microsecond before ts: a read marker
// there leaves the message at ts unread.
func TSBefore(ts string) string {
	s, u := splitTS(ts)
	if u == 0 {
		s, u = s-1, 999999
	} else {
		u--
	}
	return fmt.Sprintf("%d.%06d", s, u)
}
