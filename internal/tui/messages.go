package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// systemSubtypes are rendered as dim one-liners rather than chat messages.
var systemSubtypes = map[string]bool{
	"channel_join": true, "channel_leave": true, "group_join": true, "group_leave": true,
	"channel_topic": true, "channel_purpose": true, "channel_name": true,
	"group_topic": true, "group_purpose": true, "group_name": true,
	"channel_archive": true, "channel_unarchive": true, "pinned_item": true,
	"bot_add": true, "bot_remove": true,
}

// block is the rendered lines of one message plus where they start.
type block struct {
	lines []string
	start int
}

// renderList draws a message list into width×height, keeping the selected
// message in view. In a thread every reply is shown in full.
func (m *Model) renderList(l *msgList, width, height int, active, inThread bool) string {
	if l == nil || (!l.loaded && len(l.msgs) == 0) {
		return placeholder("loading…", width, height)
	}
	if len(l.msgs) == 0 {
		return placeholder("no messages yet", width, height)
	}

	sel := l.sel
	if sel < 0 || sel >= len(l.msgs) {
		sel = len(l.msgs) - 1
	}

	var all []string
	blocks := make([]block, len(l.msgs))
	if l.hasMore && !inThread {
		hint := "k at the top loads older messages"
		if l.loading {
			hint = "loading older messages…"
		}
		all = append(all, sFaint.Render(center(hint, width)))
	}
	var prev *backend.Message
	for i := range l.msgs {
		msg := &l.msgs[i]
		var pre []string
		if prev == nil || !sameDay(prev.Time(), msg.Time()) {
			pre = append(pre, sDate.Render(center(dayLabel(msg.Time()), width)))
		}
		isNew := l.newSince != "" && backend.TSLess(l.newSince, msg.TS) && (prev == nil || !backend.TSLess(l.newSince, prev.TS))
		if isNew {
			pre = append(pre, sNewLine.Render(strings.Repeat("─", max(0, width-6))+" new"))
		}
		grouped := prev != nil && len(pre) == 0 && !(inThread && i == 1) &&
			senderKey(*prev) == senderKey(*msg) &&
			!systemSubtypes[prev.SubType] && !systemSubtypes[msg.SubType] &&
			msg.Time().Sub(prev.Time()) < 5*time.Minute
		body := m.messageLines(*msg, width-2, !grouped, inThread)

		bar := "  "
		if i == sel {
			if active {
				bar = sBar.Render("▌") + " "
			} else {
				bar = sBarDim.Render("▌") + " "
			}
		}
		all = append(all, pre...)
		blocks[i].start = len(all)
		for _, b := range body {
			all = append(all, bar+b)
		}
		blocks[i].lines = body
		prev = msg
	}

	// Scroll: follow the bottom, or keep the selected block on screen.
	total := len(all)
	maxTop := max(0, total-height)
	if l.sel < 0 {
		l.top = maxTop
	} else {
		b := blocks[sel]
		end := b.start + len(b.lines)
		if b.start < l.top {
			l.top = b.start
		}
		if end > l.top+height {
			l.top = max(b.start, end-height)
		}
		if sel == 0 {
			l.top = 0 // show the "load older" hint and date line
		}
	}
	l.top = min(max(l.top, 0), maxTop)

	view := all[l.top:min(total, l.top+height)]
	pad := make([]string, 0, height)
	for range height - len(view) {
		pad = append(pad, "")
	}
	return strings.Join(append(pad, view...), "\n")
}

// messageLines renders one message, wrapped to width.
func (m *Model) messageLines(msg backend.Message, width int, header, inThread bool) []string {
	var lines []string
	if systemSubtypes[msg.SubType] {
		text := render.Mrkdwn(msg.Text, m, mdStyles)
		t := sTime.Render(msg.Time().Format(m.cfg.TimeFormat))
		return wrap(sSystem.Render("→ ")+text+"  "+t, width)
	}
	if header {
		name := nameStyle(senderKey(msg)).Render(m.senderName(msg))
		if msg.User == "" && (msg.BotName != "" || msg.BotID != "") {
			name += " " + sFaint.Render("APP")
		}
		head := name + "  " + sTime.Render(msg.Time().Format(m.cfg.TimeFormat))
		if msg.Pinned {
			head += "  " + sWarn.Render(pinIcon+" pinned")
		}
		lines = append(lines, head)
	}
	text := render.Mrkdwn(msg.Text, m, mdStyles)
	if msg.Edited {
		text += " " + sFaint.Render("(edited)")
	}
	if msg.Pinned && !header {
		text += " " + sWarn.Render(pinIcon+" pinned")
	}
	if strings.TrimSpace(msg.Text) != "" || msg.Edited || (msg.Pinned && !header) {
		lines = append(lines, wrap(text, width)...)
	}
	for _, f := range msg.Files {
		lines = append(lines, m.imageLines(f, width)...)
	}
	if len(msg.Reactions) > 0 {
		var chips []string
		for _, r := range mergeSkinTones(msg.Reactions) {
			chip := fmt.Sprintf("%s %d", m.emojiChip(r.Name), r.Count)
			style := sReaction
			for _, u := range r.Users {
				if u == m.client.SelfID {
					style = sReactSelf
				}
			}
			chips = append(chips, style.Render(chip))
		}
		lines = append(lines, wrap(strings.Join(chips, " "), width)...)
	}
	if !inThread && msg.ReplyCount > 0 {
		s := fmt.Sprintf("%d %s", msg.ReplyCount, plural(msg.ReplyCount, "reply", "replies"))
		if msg.LatestReply != "" {
			s += sDim.Render(" · last " + relTime(backend.TSTime(msg.LatestReply), m.cfg.TimeFormat))
		}
		lines = append(lines, sThread.Render(s))
	}
	if len(lines) == 0 {
		lines = append(lines, sFaint.Render("(empty message)"))
	}
	return lines
}

func wrap(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, hang(line, width)...)
	}
	return linksPerLine(out)
}

// reItem is the start of a list item: "• ", "  ◦ ", "- ", "12. ".
var reItem = regexp.MustCompile(`^ *(?:[•◦▪‣-]|\d{1,3}[.)]) +`)

// hang wraps one line; a list item's later lines line up under its text,
// not under the bullet, as in Slack.
func hang(line string, width int) []string {
	n := len(reItem.FindString(line)) // the bullet is plain text, so bytes line up
	pre := line[:n]
	w := ansi.StringWidth(pre)
	if n == 0 || width-w < 10 {
		return strings.Split(ansi.Wrap(line, width, ""), "\n")
	}
	lines := strings.Split(ansi.Wrap(line[n:], width-w, ""), "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = pre + lines[i]
		} else {
			lines[i] = strings.Repeat(" ", w) + lines[i]
		}
	}
	return lines
}

var reEsc = regexp.MustCompile("\x1b\\[[0-9;:]*m|\x1b]8;[^;\x1b]*;([^\x1b]*)\x1b\\\\")

// linksPerLine makes every wrapped line self-contained: colours, underline
// and hyperlinks still open at the end of a line are closed there and
// reopened on the next. Wrapping alone leaves them open across the newline,
// and the terminal then underlines (or links) the rest of that row and
// whatever is drawn next to it, like the neighbouring pane.
func linksPerLine(lines []string) []string {
	carry := ""
	for i, l := range lines {
		l = carry + l
		var sgr []string
		link := ""
		for _, m := range reEsc.FindAllStringSubmatch(l, -1) {
			switch seq := m[0]; {
			case strings.HasPrefix(seq, "\x1b]8;"):
				link = ""
				if m[1] != "" {
					link = seq
				}
			case seq == "\x1b[m" || seq == "\x1b[0m":
				sgr = sgr[:0]
			default:
				sgr = append(sgr, seq)
			}
		}
		carry = strings.Join(sgr, "") + link
		if len(sgr) > 0 {
			l += "\x1b[0m"
		}
		if link != "" {
			l += "\x1b]8;;\x1b\\"
		}
		lines[i] = l
	}
	return lines
}

func placeholder(s string, width, height int) string {
	lines := make([]string, height)
	if height > 0 {
		lines[height/2] = sDim.Render(center(s, width))
	}
	return strings.Join(lines, "\n")
}

func center(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return ansi.Truncate(s, width, "…")
	}
	return strings.Repeat(" ", (width-w)/2) + s
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func dayLabel(t time.Time) string {
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "Today"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday"
	case t.Year() == now.Year():
		return t.Format("Monday, 2 January")
	default:
		return t.Format("Monday, 2 January 2006")
	}
}

func relTime(t time.Time, layout string) string {
	if sameDay(t, time.Now()) {
		return t.Format(layout)
	}
	return t.Format("2 Jan " + layout)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// mergeSkinTones combines reactions that differ only by skin tone, since
// skin tones are shown as the base emoji: 🙏 2 and 🙏 1 become 🙏 3.
func mergeSkinTones(rs []backend.Reaction) []backend.Reaction {
	var out []backend.Reaction
	at := map[string]int{}
	for _, r := range rs {
		base := render.BaseEmoji(r.Name)
		if i, ok := at[base]; ok {
			out[i].Count += r.Count
			out[i].Users = append(out[i].Users, r.Users...)
			continue
		}
		at[base] = len(out)
		out = append(out, backend.Reaction{Name: base, Count: r.Count, Users: append([]string(nil), r.Users...)})
	}
	return out
}
