package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

func (m *Model) View() string {
	m.viewSeq++ // images drawn below record it; only those animate
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.bootErr != nil {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			sErr.Render("Could not load Slack: "+m.bootErr.Error())+"\n\n"+sDim.Render("ctrl+c to quit"))
	}
	if !m.booted {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			sDim.Render("Loading "+m.client.TeamName+"…"))
	}

	if m.viewer != nil {
		return m.renderViewer()
	}

	h := m.height - 1
	sw := min(m.cfg.SidebarWidth, m.width/3)
	mainW := m.width - sw - 1
	if m.sideHidden {
		mainW = m.width
	}
	// Every pane is framed; the focused one in the accent colour.
	iw, ih := mainW-2, h-2
	onMain := m.focus != fSidebar

	var main string
	split := false // the conversation and thread side by side, framed apart
	switch {
	case m.switcher != nil:
		main = lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Top, "\n\n"+m.renderSwitcher(iw, ih))
	case m.picker != nil:
		main = lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Top, "\n\n"+m.renderPicker(iw, ih))
	case m.opener != nil:
		main = lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Top, "\n\n"+m.renderOpener(iw))
	case m.profile != nil:
		main = lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Top, "\n\n"+m.renderProfile(iw))
	case m.help != nil:
		main = lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Top, "\n"+m.renderHelp(iw, ih-1))
	case m.search != nil && m.search.open:
		main = m.renderSearch(iw, ih)
	case m.pins != nil:
		main = m.renderPins(iw, ih)
	case m.sched != nil:
		main = m.renderScheduled(iw, ih)
	case m.chans != nil:
		main = m.renderChans(iw, ih)
	case m.members != nil:
		main = m.renderMembers(iw, ih)
	case m.shared != nil:
		main = m.renderShared(iw, ih)
	case m.current == unreadsID:
		main = m.renderUnreads(iw, ih)
	case m.thread != nil && mainW >= 80:
		tw := mainW * 45 / 100
		cw := mainW - tw
		split = true
		main = lipgloss.JoinHorizontal(lipgloss.Top,
			frame(m.renderConvPane(cw-2, ih), cw, h, m.workBorder(m.focus == fMessages)),
			frame(m.renderThreadPane(tw-2, ih), tw, h, m.workBorder(m.focus == fThread)))
	case m.thread != nil:
		main = m.renderThreadPane(iw, ih)
	default:
		main = m.renderConvPane(iw, ih)
	}
	if !split {
		main = frame(main, mainW, h, m.workBorder(onMain))
	}

	var body string
	switch {
	case m.sideHidden:
		body = main
	case m.sideRight:
		body = lipgloss.JoinHorizontal(lipgloss.Top, main, frame(m.renderSidebar(sw-1, ih), sw+1, h, border(!onMain)))
	default:
		body = lipgloss.JoinHorizontal(lipgloss.Top, frame(m.renderSidebar(sw-1, ih), sw+1, h, border(!onMain)), main)
	}
	if m.cmd != nil {
		return m.overlayCmdCompletion(body) + "\n" + m.renderCmdLine()
	}
	return body + "\n" + m.renderStatus()
}

// paneBorder is a frame's look: its colour and line.
type paneBorder struct {
	st    lipgloss.Style
	lines lipgloss.Border
}

// border is the accent colour on the focused pane, faint on the others.
func border(focused bool) paneBorder {
	if focused {
		return paneBorder{sBorderFocus, lipgloss.RoundedBorder()}
	}
	return paneBorder{sBorder, lipgloss.RoundedBorder()}
}

// workBorder is border for the panes right of the sidebar, thick in the
// alert colour while a watched channel has news.
func (m *Model) workBorder(focused bool) paneBorder {
	if m.alert != nil {
		return paneBorder{sBorderAlert, lipgloss.ThickBorder()}
	}
	return border(focused)
}

// frame draws a border round s, fitted to (w-2)×(h-2).
func frame(s string, w, h int, b paneBorder) string {
	st, ln := b.st, b.lines
	left, right := st.Render(ln.Left), st.Render(ln.Right)
	out := make([]string, 0, h)
	out = append(out, st.Render(ln.TopLeft+strings.Repeat(ln.Top, max(0, w-2))+ln.TopRight))
	for _, l := range strings.Split(fit(s, w-2, h-2), "\n") {
		out = append(out, left+l+right)
	}
	out = append(out, st.Render(ln.BottomLeft+strings.Repeat(ln.Bottom, max(0, w-2))+ln.BottomRight))
	return strings.Join(out, "\n")
}

// fit forces s to exactly w×h cells so panes line up.
func fit(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		l = ansi.Truncate(l, w, "")
		if pad := w - ansi.StringWidth(l); pad > 0 {
			l += strings.Repeat(" ", pad)
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}

func rule(w int, active bool) string {
	if active {
		return sBar.Render(strings.Repeat("─", w))
	}
	return sFaint.Render(strings.Repeat("─", w))
}

func (m *Model) convTitle(c *backend.Conversation) string {
	switch c.Kind {
	case backend.KindChannel:
		return "# " + c.Name
	case backend.KindPrivate:
		return lockIcon + " " + c.Name
	default:
		return m.convLabel(c)
	}
}

func (m *Model) renderConvPane(w, h int) string {
	c := m.convs[m.current]
	if c == nil {
		return placeholder("conversation not found", w, h)
	}
	header := []string{m.paneHeader(m.convHeader(c), "MESSAGES", m.focus == fMessages, w), rule(w, false)}

	isTarget := !(m.focus == fThread && m.thread != nil)
	who := "#" + c.Name
	if c.IsDM() {
		who = m.convLabel(c)
	}
	input := m.renderInput(w, isTarget, "Message "+who)
	bodyH := max(1, h-len(header)-lineCount(input))
	list := m.renderList(m.lists[m.current], w, bodyH, m.focus == fMessages, false)
	return strings.Join(append(header, list, input), "\n")
}

// sep is the divider between the parts of a pane header.
func sep() string { return sFaint.Render(" │ ") }

// convHeader is the top line of a conversation: for a person, presence,
// name, status and their local time; for a channel, its name, topic and
// size.
func (m *Model) convHeader(c *backend.Conversation) string {
	parts := []string{sTitle.Render(m.convTitle(c))}
	switch {
	case c.Kind == backend.KindDM && c.UserID != m.client.SelfID:
		dot := sFaint.Render(awayDot)
		if m.presence[c.UserID] {
			dot = sPresent.Render(onlineDot)
		}
		parts[0] = dot + " " + parts[0]
		u := m.users[c.UserID]
		if _, text := u.Status(time.Now()); text != "" {
			// "In a meeting • Google Calendar": the source is noise here.
			text, _, _ = strings.Cut(text, " • ")
			st := sWarn.Render(strings.ReplaceAll(render.Mrkdwn(text, m, mdStyles), "\n", " "))
			if u.StatusExpires > 0 {
				st += sDim.Render(" until " + clock(time.Unix(u.StatusExpires, 0), m.cfg.TimeFormat))
			}
			parts = append(parts, st)
		}
		if t, ok := theirClock(u, time.Now()); ok {
			parts = append(parts, sDim.Render("local "+t.Format(m.cfg.TimeFormat)))
		}
	case c.Kind == backend.KindGroup:
		parts[0] = sGroup.Render(groupIcon) + " " + parts[0]
	default:
		if c.Topic != "" {
			parts = append(parts, sTopic.Render(strings.ReplaceAll(render.Mrkdwn(c.Topic, m, mdStyles), "\n", " ")))
		}
		if c.Members > 0 {
			parts = append(parts, sDim.Render(fmt.Sprintf("%d %s", c.Members, plural(c.Members, "member", "members"))))
		}
	}
	if n := m.scheduledIn(c.ID); n > 0 {
		parts = append(parts, sWarn.Render(fmt.Sprintf("%s %d scheduled", schedIcon, n)))
	}
	return strings.Join(parts, sep())
}

// paneHeader puts the pane's name at the right of its title line, in the
// accent colour while the pane has the keys.
func (m *Model) paneHeader(title, name string, focused bool, w int) string {
	label := sFaint.Render(name)
	if focused {
		label = sBar.Render(name)
	}
	room := w - 2 - lipgloss.Width(label) - 2
	title = ansi.Truncate(" "+title, room, "…")
	return title + strings.Repeat(" ", max(1, w-1-lipgloss.Width(title)-lipgloss.Width(label))) + label
}

func (m *Model) renderThreadPane(w, h int) string {
	t := m.thread
	title := sTitle.Render("Thread")
	if c := m.convs[t.conv]; c != nil {
		title += sDim.Render("  in " + m.convTitle(c))
	}
	header := []string{m.paneHeader(title, "THREAD", m.focus == fThread, w), rule(w, false)}
	input := m.renderInput(w, m.focus == fThread, "Reply in thread")
	bodyH := max(1, h-len(header)-lineCount(input))
	list := m.renderList(&t.list, w, bodyH, m.focus == fThread, true)
	return strings.Join(append(header, list, input), "\n")
}

// renderInput is the message box at the foot of a pane, inset a column
// from its edges. Out of insert mode it shows a placeholder, and in the
// pane it will send to, the keys; in insert mode its border takes the
// accent colour and labels about the message go into the top edge.
func (m *Model) renderInput(w int, isTarget bool, placeholder string) string {
	bw := w - 2 // the box, inset a column on each side
	if !m.insert || !isTarget {
		text := sFaint.Render("› " + placeholder)
		if isTarget {
			// As many hints as fit, the first ones first.
			hints := [][2]string{{"i", "type"}, {"⏎", "send"}, {"alt+⏎", "newline"}}
			for n := len(hints); n > 0; n-- {
				hint := keyHints(hints[:n])
				if gap := bw - 4 - lipgloss.Width(text) - lipgloss.Width(hint); gap >= 2 {
					text += strings.Repeat(" ", gap) + hint
					break
				}
			}
		}
		return inputBox([]string{text}, bw, sBorder, nil)
	}
	m.input.SetWidth(bw - 6)
	m.fitInput() // the width may have changed, and with it the wrapping
	var labels []string
	if m.editing != nil {
		labels = append(labels, sWarn.Render(" editing · enter save · esc cancel "))
	}
	if label := m.broadcastLabel(); label != "" {
		if m.broadcast {
			labels = append(labels, sWarn.Render(label))
		} else {
			labels = append(labels, sFaint.Render(label))
		}
	}
	if label := m.attachLabel(); label != "" {
		labels = append(labels, sWarn.Render(label))
	}
	rows := strings.Split(m.input.View(), "\n")
	for i := range rows {
		if i == 0 {
			rows[i] = sBar.Render("›") + " " + rows[i]
		} else {
			rows[i] = "  " + rows[i]
		}
	}
	box := inputBox(rows, bw, sBorderFocus, labels)
	if m.complete != nil {
		box = strings.Join(m.renderCompletion(w), "\n") + "\n" + box
	}
	return box
}

// inputBox frames rows in a box w wide, indented one column, with labels
// set into its top edge.
func inputBox(rows []string, w int, st lipgloss.Style, labels []string) string {
	top := st.Render("╭─")
	for _, l := range labels {
		top += l + st.Render("─")
	}
	top = ansi.Truncate(top, w-1, "") + st.Render(strings.Repeat("─", max(0, w-1-lipgloss.Width(top)))+"╮")
	out := []string{" " + top}
	for _, r := range rows {
		r = ansi.Truncate(r, w-4, "")
		out = append(out, " "+st.Render("│")+" "+r+strings.Repeat(" ", max(0, w-4-lipgloss.Width(r)))+" "+st.Render("│"))
	}
	out = append(out, " "+st.Render("╰"+strings.Repeat("─", max(0, w-2))+"╯"))
	return strings.Join(out, "\n")
}

// keyHints renders "k desc · k desc": keys in the text colour, the rest dim.
func keyHints(hints [][2]string) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = sKeyHint.Render(h[0]) + sDim.Render(" "+h[1])
	}
	return strings.Join(parts, sDim.Render(" · "))
}

// broadcastLabel is the bar above a thread reply being written: whether it
// also goes to the channel, as Slack's checkbox.
func (m *Model) broadcastLabel() string {
	conv, thread := m.inputTarget()
	c := m.convs[conv]
	if thread == "" || c == nil || m.editing != nil {
		return ""
	}
	where := "#" + c.Name
	if c.IsDM() {
		where = "the conversation"
	}
	if m.broadcast {
		return " also sending to " + where + " · ctrl+o undo "
	}
	return " ctrl+o also send to " + where + " "
}

func lineCount(s string) int { return strings.Count(s, "\n") + 1 }

func (m *Model) renderUnreads(w, h int) string {
	convs := m.unreadConvs()
	title := sTitle.Render("Unreads")
	if len(convs) > 0 {
		title += sDim.Render(fmt.Sprintf("  %d %s", len(convs), plural(len(convs), "conversation", "conversations")))
	}
	header := []string{title, rule(w, m.focus == fMessages)}
	footer := sFaint.Render("  ") + sKey.Render("enter") + sFaint.Render(" open  ") +
		sKey.Render("r") + sFaint.Render(" mark read  ") + sKey.Render("u") + sFaint.Render(" next unread")
	bodyH := h - len(header) - 2

	if len(convs) == 0 {
		msg := "All caught up ✓"
		if m.infoPending > 0 {
			msg = fmt.Sprintf("checking for unreads… %d/%d", m.infoTotal-m.infoPending, m.infoTotal)
		}
		return strings.Join(append(header, placeholder(msg, w, bodyH), "", footer), "\n")
	}

	sel := min(max(m.unreads.sel, 0), len(convs)-1)
	var all []string
	var selStart, selEnd int
	for i, c := range convs {
		bar := "  "
		if i == sel {
			bar = sBar.Render("▌") + " "
			if m.focus != fMessages {
				bar = sBarDim.Render("▌") + " "
			}
			selStart = len(all)
		}
		head := sUnread.Render(m.convTitle(c)) + "  " + sTime.Render(relTime(backend.TSTime(c.Latest), m.cfg.TimeFormat))
		if c.Mentions > 0 {
			head += " " + sBadge.Render(fmt.Sprint(c.Mentions))
		}
		block := []string{head}
		msgs, ok := m.unreadMsgs[c.ID]
		switch {
		case !ok:
			block = append(block, sDim.Render("  loading…"))
		case len(msgs) == 0:
			block = append(block, sDim.Render("  (only thread replies or hidden messages)"))
		default:
			const show = 5
			if len(msgs) > show {
				block = append(block, sDim.Render(fmt.Sprintf("  … %d earlier", len(msgs)-show)))
				msgs = msgs[len(msgs)-show:]
			}
			for _, msg := range msgs {
				name := nameStyle(senderKey(msg)).Render(m.senderName(msg))
				text := strings.ReplaceAll(render.Mrkdwn(msg.Text, m, mdStyles), "\n", " ")
				if strings.TrimSpace(msg.Text) == "" && len(msg.Files) > 0 {
					text = sDim.Render(msg.Files[0].Name)
				}
				line := name + " " + sTime.Render(msg.Time().Format(m.cfg.TimeFormat)) + "  " + text
				wrapped := wrap(line, w-4)
				if len(wrapped) > 3 {
					wrapped = append(wrapped[:3:3], sFaint.Render("…"))
				}
				for _, l := range wrapped {
					block = append(block, "  "+l)
				}
			}
		}
		for _, l := range block {
			all = append(all, bar+l)
		}
		if i == sel {
			selEnd = len(all)
		}
		all = append(all, "")
	}

	u := &m.unreads
	if selStart < u.top {
		u.top = selStart
	}
	if selEnd > u.top+bodyH {
		u.top = max(selStart, selEnd-bodyH)
	}
	u.top = min(max(u.top, 0), max(0, len(all)-bodyH))
	view := all[u.top:min(len(all), u.top+bodyH)]
	for len(view) < bodyH {
		view = append(view, "")
	}
	return strings.Join(append(append(header, view...), rule(w, false), footer), "\n")
}

func (m *Model) renderStatus() string {
	mode := sModeN.Render("NORMAL")
	if m.insert {
		mode = sModeI.Render("INSERT")
	} else if m.search != nil && m.search.typing {
		mode = sModeI.Render("SEARCH")
	}
	if badge := m.modeBadge(); badge != "" {
		mode += " " + badge
	}
	var conn string
	switch m.conn {
	case "connected":
		conn = sOK.Render("● live")
	case "error":
		conn = sErr.Render("✕ offline")
	default:
		conn = sWarn.Render("○ connecting")
	}
	conn += sFaint.Render(" │ ") + sBar.Render(paneNames[m.focus])
	parts := append([]string{mode, conn}, m.meStatus()...)
	if m.confirm != nil {
		parts = append(parts, sWarn.Render(m.confirm.prompt))
	}
	if m.infoPending > 0 {
		parts = append(parts, sDim.Render(fmt.Sprintf("checking unreads %d/%d", m.infoTotal-m.infoPending, m.infoTotal)))
	}
	if m.status != "" {
		if m.statusErr {
			parts = append(parts, sErr.Render(m.status))
		} else {
			parts = append(parts, sDim.Render(m.status))
		}
	}
	// The key hints keep their place: the left side gives way first, down to
	// the mode and connection, then hints drop from the end.
	// A y/n question needs the room more than the keys do.
	hints := m.statusHints()
	right := keyHints(hints) + " "
	if m.confirm != nil {
		hints, right = nil, ""
	}
	for len(hints) > 1 && lipgloss.Width(mode+"  "+conn)+2+lipgloss.Width(right) > m.width {
		hints = hints[:len(hints)-1]
		right = keyHints(hints) + " "
	}
	room := m.width - lipgloss.Width(right) - 2
	if room < lipgloss.Width(mode+"  "+conn) {
		return ansi.Truncate(strings.Join(parts, "  "), m.width, "…")
	}
	left := ansi.Truncate(strings.Join(parts, "  "), room, "…")
	return left + strings.Repeat(" ", m.width-lipgloss.Width(left)-lipgloss.Width(right)) + right
}

// paneNames name the focused pane in the status bar; tabNames the pane tab
// goes to.
var (
	paneNames = map[focus]string{fSidebar: "sidebar", fMessages: "message", fThread: "thread"}
	tabNames  = map[focus]string{fSidebar: "sidebar", fMessages: "messages", fThread: "thread"}
)

// statusHints are the keys for what has focus: the message box, the
// sidebar, or a selected message; otherwise the general ones.
func (m *Model) statusHints() [][2]string {
	switch {
	case m.insert:
		return [][2]string{{"esc", "normal"}, {"⏎", "send"}, {"alt+⏎", "newline"}}
	case m.switcher != nil || m.overlayOpen():
	case m.focus == fSidebar:
		return [][2]string{{"⏎", "open"}, {"r", "mark read"}, {"M", "mute"}, {"tab", tabNames[m.nextFocus()]}, {"?", "help"}}
	case m.focus == fThread && m.thread != nil && len(m.thread.list.msgs) > 0:
		return [][2]string{{"i", "reply"}, {"+", "react"}, {"c", "copy"}, {"tab", tabNames[m.nextFocus()]}, {"?", "help"}}
	case m.focus == fMessages && m.current != unreadsID && m.lists[m.current] != nil && len(m.lists[m.current].msgs) > 0:
		return [][2]string{{"t", "reply"}, {"+", "react"}, {"c", "copy"}, {"tab", tabNames[m.nextFocus()]}, {"?", "help"}}
	}
	return [][2]string{{"ctrl+k", "jump"}, {"/", "search"}, {":", "command"}, {"?", "help"}}
}

// overlayOpen is whether a pane covers the conversation.
func (m *Model) overlayOpen() bool {
	return m.picker != nil || m.opener != nil || m.profile != nil ||
		m.help != nil || (m.search != nil && m.search.open) || m.pins != nil || m.sched != nil ||
		m.chans != nil || m.members != nil || m.shared != nil
}

// nextFocus is the pane tab moves to.
func (m *Model) nextFocus() focus {
	n := 2
	if m.thread != nil {
		n = 3
	}
	f := focus((int(m.focus) + 1) % n)
	if f == fSidebar && m.sideHidden {
		f = focus((int(f) + 1) % n)
	}
	return f
}
