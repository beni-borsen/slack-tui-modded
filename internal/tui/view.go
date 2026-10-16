package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

func (m *Model) View() string {
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

	var main string
	switch {
	case m.switcher != nil:
		main = lipgloss.Place(mainW, h, lipgloss.Center, lipgloss.Top, "\n\n"+m.renderSwitcher(mainW, h))
	case m.picker != nil:
		main = lipgloss.Place(mainW, h, lipgloss.Center, lipgloss.Top, "\n\n"+m.renderPicker(mainW, h))
	case m.opener != nil:
		main = lipgloss.Place(mainW, h, lipgloss.Center, lipgloss.Top, "\n\n"+m.renderOpener(mainW))
	case m.help:
		main = lipgloss.Place(mainW, h, lipgloss.Center, lipgloss.Center, m.renderHelp())
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

	body := lipgloss.JoinHorizontal(lipgloss.Top, fit(m.renderSidebar(sw, h), sw, h), vline(h), fit(main, mainW, h))
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
		return "⊘ " + c.Name
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
	top := rule(w, true)
	if m.complete != nil {
		top = strings.Join(m.renderCompletion(w), "\n") + "\n" + top
	}
	if m.editing != nil {
		label := " editing · enter save · esc cancel "
		bar := sBar.Render("──") + sWarn.Render(label) + sBar.Render(strings.Repeat("─", max(0, w-2-lipgloss.Width(label))))
		top = strings.TrimSuffix(top, rule(w, true)) + bar
	}
	if label := m.attachLabel(); label != "" {
		bar := sBar.Render("──") + sWarn.Render(label) + sBar.Render(strings.Repeat("─", max(0, w-2-lipgloss.Width(label))))
		top = strings.TrimSuffix(top, rule(w, true)) + bar
	}
	return top + "\n" + indent(m.input.View(), "  ")
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
				name := nameStyle(msg.User + msg.BotName).Render(m.senderName(msg))
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
	parts := []string{mode, conn}
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
	right := sDim.Render("ctrl+k jump · ? help ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) renderHelp() string {
	rows := [][2]string{
		{"ctrl+k / space", "jump to conversation"},
		{"j k  ↑ ↓", "move"},
		{"g G", "top / bottom"},
		{"ctrl+d ctrl+u", "half page"},
		{"h l  tab", "move between panes"},
		{"enter", "open conversation / thread"},
		{"i a", "write a message (in the focused pane)"},
		{"enter  (insert)", "send"},
		{"alt+enter ctrl+j", "new line"},
		{"ctrl+v  (insert)", "attach clipboard image (again for more)"},
		{"ctrl+b alt+i  (insert)", "bold / italic the word at the cursor"},
		{":ta @an #de  (insert)", "complete emoji, people, channels"},
		{"esc", "back to normal mode"},
		{"u", "next unread conversation"},
		{"r", "mark conversation read"},
		{"z  enter on header", "fold / unfold sidebar section"},
		{"+", "react to the message"},
		{"c", "copy the message text"},
		{"e", "edit your message"},
		{"↑  (empty input)", "edit your last message"},
		{"D", "delete your message"},
		{"v", "view images full-window: ← → browse, o open, esc close"},
		{"o", "open the message's file or link (a chooser when there are several)"},
		{"x  (thread)", "close thread"},
		{"q", "quit"},
	}
	var lines []string
	for _, r := range rows {
		lines = append(lines, sKey.Render(fmt.Sprintf("%-18s", r[0]))+"  "+sNormal.Render(r[1]))
	}
	return sBox.Render(sTitle.Render("Keys") + "\n\n" + strings.Join(lines, "\n") + "\n\n" + sDim.Render("any key to close"))
}
