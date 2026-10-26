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

	var main string
	switch {
	case m.switcher != nil:
		main = lipgloss.Place(mainW, h, lipgloss.Center, lipgloss.Top, "\n\n"+m.renderSwitcher(mainW, h))
	case m.picker != nil:
		main = lipgloss.Place(mainW, h, lipgloss.Center, lipgloss.Top, "\n\n"+m.renderPicker(mainW, h))
	case m.opener != nil:
		main = lipgloss.Place(mainW, h, lipgloss.Center, lipgloss.Top, "\n\n"+m.renderOpener(mainW))
	case m.profile != nil:
		main = lipgloss.Place(mainW, h, lipgloss.Center, lipgloss.Top, "\n\n"+m.renderProfile(mainW))
	case m.help != nil:
		main = lipgloss.Place(mainW, h, lipgloss.Center, lipgloss.Top, "\n"+m.renderHelp(mainW, h-1))
	case m.search != nil && m.search.open:
		main = m.renderSearch(mainW, h)
	case m.pins != nil:
		main = m.renderPins(mainW, h)
	case m.sched != nil:
		main = m.renderScheduled(mainW, h)
	case m.chans != nil:
		main = m.renderChans(mainW, h)
	case m.members != nil:
		main = m.renderMembers(mainW, h)
	case m.shared != nil:
		main = m.renderShared(mainW, h)
	case m.current == unreadsID:
		main = m.renderUnreads(mainW, h)
	case m.thread != nil && mainW >= 80:
		tw := mainW * 45 / 100
		cw := mainW - tw - 1
		main = lipgloss.JoinHorizontal(lipgloss.Top,
			fit(m.renderConvPane(cw, h), cw, h), vline(h), fit(m.renderThreadPane(tw, h), tw, h))
	case m.thread != nil:
		main = m.renderThreadPane(mainW, h)
	default:
		main = m.renderConvPane(mainW, h)
	}

	var body string
	switch {
	case m.sideHidden:
		body = fit(main, mainW, h)
	case m.sideRight:
		body = lipgloss.JoinHorizontal(lipgloss.Top, fit(main, mainW, h), vline(h), fit(m.renderSidebar(sw, h), sw, h))
	default:
		body = lipgloss.JoinHorizontal(lipgloss.Top, fit(m.renderSidebar(sw, h), sw, h), vline(h), fit(main, mainW, h))
	}
	if m.cmd != nil {
		return m.overlayCmdCompletion(body) + "\n" + m.renderCmdLine()
	}
	return body + "\n" + m.renderStatus()
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

func vline(h int) string {
	return sFaint.Render(strings.TrimSuffix(strings.Repeat("│\n", h), "\n"))
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
	title := sTitle.Render(m.convTitle(c))
	if c.Kind == backend.KindDM && c.UserID != m.client.SelfID {
		if m.presence[c.UserID] {
			title += " " + sOK.Render(onlineDot)
		}
		if line := m.statusLine(c.UserID); line != "" {
			if exp := m.users[c.UserID].StatusExpires; exp > 0 {
				line += sDim.Render(" until " + clock(time.Unix(exp, 0), m.cfg.TimeFormat))
			}
			title += "  " + line
		}
	}
	if n := m.scheduledIn(c.ID); n > 0 {
		title += "  " + sWarn.Render(fmt.Sprintf("%s %d scheduled", schedIcon, n))
	}
	if c.Topic != "" {
		title += "  " + sTopic.Render(strings.ReplaceAll(render.Mrkdwn(c.Topic, m, mdStyles), "\n", " "))
	}
	header := []string{ansi.Truncate(title, w, "…"), rule(w, m.focus == fMessages)}

	isTarget := !(m.focus == fThread && m.thread != nil)
	input := m.renderInput(w, isTarget, "message "+m.convTitle(c))
	bodyH := max(1, h-len(header)-lineCount(input))
	list := m.renderList(m.lists[m.current], w, bodyH, m.focus == fMessages, false)
	return strings.Join(append(header, list, input), "\n")
}

func (m *Model) renderThreadPane(w, h int) string {
	t := m.thread
	where := ""
	if c := m.convs[t.conv]; c != nil {
		where = sDim.Render("  in " + m.convTitle(c))
	}
	header := []string{ansi.Truncate(sTitle.Render("Thread")+where, w, "…"), rule(w, m.focus == fThread)}
	input := m.renderInput(w, m.focus == fThread, "reply in thread")
	bodyH := max(1, h-len(header)-lineCount(input))
	list := m.renderList(&t.list, w, bodyH, m.focus == fThread, true)
	return strings.Join(append(header, list, input), "\n")
}

// renderInput shows the text box under the pane it will send to, and a
// hint under the other one.
func (m *Model) renderInput(w int, isTarget bool, hint string) string {
	if !isTarget {
		return rule(w, false) + "\n" + sFaint.Render(" ")
	}
	if !m.insert {
		return rule(w, false) + "\n" + sFaint.Render("  "+sKey.Render("i")+sFaint.Render(" "+hint))
	}
	m.input.SetWidth(w - 2)
	m.fitInput() // the width may have changed, and with it the wrapping
	// Labels go into the rule above the box: "── editing … ── image … ──".
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
	top := rule(w, true)
	if len(labels) > 0 {
		bar := sBar.Render("──") + strings.Join(labels, sBar.Render("──"))
		top = ansi.Truncate(bar+sBar.Render(strings.Repeat("─", max(0, w-lipgloss.Width(bar)))), w, "")
	}
	if m.complete != nil {
		top = strings.Join(m.renderCompletion(w), "\n") + "\n" + top
	}
	return top + "\n" + indent(m.input.View(), "  ")
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

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
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
	var conn string
	switch m.conn {
	case "connected":
		conn = sOK.Render("● live")
	case "error":
		conn = sErr.Render("✕ offline")
	default:
		conn = sWarn.Render("○ connecting")
	}
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
	left := strings.Join(parts, "  ")
	right := sDim.Render("ctrl+k jump · / search · : command · ? help ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}
