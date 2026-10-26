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
	// The selected message has a bar down its left side; in the focused
	// pane its rows are also tinted across the whole width.
	selRow := func(line string) string {
		if !active {
			return sBarDim.Render("▌") + " " + line
		}
		pad := strings.Repeat(" ", max(0, width-2-ansi.StringWidth(line)))
		return sBar.Render("▌") + tint(" "+line+pad, selBgOn)
	}
	var prev *backend.Message
	for i := range l.msgs {
		msg := &l.msgs[i]
		var pre []string
		if prev == nil || !sameDay(prev.Time(), msg.Time()) {
			pre = append(pre, dayRule(dayLabel(msg.Time()), width))
		} else if gap := msg.Time().Sub(prev.Time()); gap > 30*time.Minute {
			pre = append(pre, gapRule(gap, width))
		}
		isNew := l.newSince != "" && backend.TSLess(l.newSince, msg.TS) && (prev == nil || !backend.TSLess(l.newSince, prev.TS))
		if isNew {
			pre = append(pre, sNewLine.Render(strings.Repeat("─", max(0, width-6))+" new"))
		}
		grouped := prev != nil && len(pre) == 0 && !(inThread && i == 1) &&
			senderKey(*prev) == senderKey(*msg) &&
			!systemSubtypes[prev.SubType] && !systemSubtypes[msg.SubType] &&
			msg.Time().Sub(prev.Time()) < 5*time.Minute
		body := m.messageLines(*msg, width-2, !grouped, inThread, active && i == sel)

		// A blank line between one sender's turn and the next.
		if prev != nil && !grouped {
			all = append(all, "")
		}
		all = append(all, pre...)
		blocks[i].start = len(all)
		for _, b := range body {
			if i == sel {
				all = append(all, selRow(b))
			} else {
				all = append(all, "  "+b)
			}
		}
		blocks[i].lines = body
		prev = msg
	}

	// Scroll: follow the bottom, or keep the selected block on screen. A
	// block taller than the pane keeps wherever j/k scrolled it to.
	total := len(all)
	maxTop := max(0, total-height)
	b := blocks[sel]
	end := b.start + len(b.lines)
	switch {
	case l.sel < 0:
		l.top = maxTop
	case len(b.lines) > height:
		l.top = min(max(l.top, b.start), end-height)
		if sel == 0 && l.top == b.start {
			l.top = 0
		}
	default:
		if b.start < l.top {
			l.top = b.start
		}
		if end > l.top+height {
			l.top = end - height
		}
		if sel == 0 {
			l.top = 0 // show the "load older" hint and date line
		}
	}
	l.top = min(max(l.top, 0), maxTop)
	l.spans = l.spans[:0]
	for _, b := range blocks {
		l.spans = append(l.spans, span{b.start, len(b.lines)})
	}
	l.viewH, l.total = height, total

	view := append([]string(nil), all[l.top:min(total, l.top+height)]...)
	// A selected message cut off by the pane says so: its sender stays on
	// the top row, and the bottom row counts the lines still below.
	if height >= 4 && len(view) == height {
		if above := l.top - b.start; above > 0 && end > l.top {
			view[0] = selRow(m.stickyHead(l.msgs[sel], above+1, width-2, active))
		}
		if below := end - (l.top + height); below > 0 && b.start < l.top+height {
			view[height-1] = selRow(sFaint.Render(fmt.Sprintf("↓ %d more %s", below+1, plural(below+1, "line", "lines"))))
		}
	}
	pad := make([]string, 0, height)
	for range height - len(view) {
		pad = append(pad, "")
	}
	return strings.Join(append(pad, view...), "\n")
}

// stickyHead stands in for the header of a message whose top has
// scrolled out of view: who wrote it, when, and how much is above.
func (m *Model) stickyHead(msg backend.Message, above, width int, lit bool) string {
	head := m.senderLabel(msg, lit) + "  " +
		sTime.Render(msg.Time().Format(m.cfg.TimeFormat)) + "  " +
		sFaint.Render(fmt.Sprintf("↑ %d more %s", above, plural(above, "line", "lines")))
	return ansi.Truncate(head, width, "…")
}

// senderLabel is the coloured name over a message; the user's own read
// "you", in a muted tone, which on the selection's tint (lit) is too faint
// and takes the text colour instead.
func (m *Model) senderLabel(msg backend.Message, lit bool) string {
	if msg.User != "" && msg.User == m.client.SelfID {
		if lit {
			return sNormal.Bold(true).Render("you")
		}
		return sOwnName.Render("you")
	}
	return nameStyle(senderKey(msg)).Render(m.senderName(msg))
}

// messageLines renders one message, wrapped to width.
func (m *Model) messageLines(msg backend.Message, width int, header, inThread, lit bool) []string {
	var lines []string
	if systemSubtypes[msg.SubType] {
		text := render.Mrkdwn(msg.Text, m, mdStyles)
		t := sTime.Render(msg.Time().Format(m.cfg.TimeFormat))
		return wrap(sSystem.Render("→ ")+text+"  "+t, width)
	}
	if header {
		name := m.senderLabel(msg, lit)
		if msg.User == "" && (msg.BotName != "" || msg.BotID != "") {
			name += " " + sFaint.Render("APP")
		}
		head := name + "  " + sTime.Render(msg.Time().Format(m.cfg.TimeFormat))
		if msg.Pinned {
			head += "  " + sWarn.Render(pinIcon+" pinned")
		}
		lines = append(lines, head)
	}
	// The body sits two columns in, under the sender's name.
	const in = "  "
	width -= len(in)
	text := render.Mrkdwn(msg.Text, m, mdStyles)
	if msg.Edited {
		text += " " + sFaint.Render("(edited)")
	}
	if msg.Pinned && !header {
		text += " " + sWarn.Render(pinIcon+" pinned")
	}
	if strings.TrimSpace(msg.Text) != "" || msg.Edited || (msg.Pinned && !header) {
		// Your own text is a little dimmer, so the replies stand out; selected,
		// it is full strength like the rest.
		fg := otherTextOn
		if msg.User != "" && msg.User == m.client.SelfID && !lit {
			fg = ownTextOn
		}
		for _, l := range wrap(text, width) {
			lines = append(lines, in+tint(l, fg))
		}
	}
	for _, f := range msg.Files {
		for _, l := range m.imageLines(f, width) {
			lines = append(lines, in+l)
		}
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
		for _, l := range wrap(strings.Join(chips, " "), width) {
			lines = append(lines, in+l)
		}
	}
	if !inThread && msg.ReplyCount > 0 {
		s := fmt.Sprintf("%d %s", msg.ReplyCount, plural(msg.ReplyCount, "reply", "replies"))
		if msg.LatestReply != "" {
			s += sDim.Render(" · last " + relTime(backend.TSTime(msg.LatestReply), m.cfg.TimeFormat))
		}
		lines = append(lines, in+sThread.Render(s))
	}
	if len(lines) == 0 {
		lines = append(lines, in+sFaint.Render("(empty message)"))
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

// dayLabel names a day for its divider: "Today · Mon 26 Oct", "Thu 22 Oct",
// with the year when it isn't this one.
func dayLabel(t time.Time) string {
	now := time.Now()
	date := t.Format("Mon 2 Jan")
	if t.Year() != now.Year() {
		date = t.Format("Mon 2 Jan 2006")
	}
	switch {
	case sameDay(t, now):
		return "Today · " + date
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday · " + date
	}
	return date
}

// dayRule is a full-width divider with the day in the middle.
func dayRule(label string, width int) string {
	return labelRule(label, "─", sDim, width)
}

// gapRule marks a pause of more than half an hour between messages.
func gapRule(d time.Duration, width int) string {
	label := fmt.Sprintf("%d min later", int(d/time.Minute))
	if d >= time.Hour {
		label = fmt.Sprintf("%d h later", int(d.Round(time.Hour)/time.Hour))
	}
	return labelRule(label, "╌", sFaint, width)
}

func labelRule(label, line string, style lipgloss.Style, width int) string {
	w := ansi.StringWidth(label) + 2
	if w >= width {
		return style.Render(ansi.Truncate(label, width, "…"))
	}
	left := (width - w) / 2
	return sFaint.Render(strings.Repeat(line, left)) + style.Render(" "+label+" ") +
		sFaint.Render(strings.Repeat(line, width-w-left))
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
