package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	if k.String() == "ctrl+c" {
		return m.quit()
	}
	if m.viewer != nil {
		return m.viewerKey(k)
	}
	if m.switcher != nil {
		return m.switcherKey(k)
	}
	// The command line and y/n questions can open over the panes below
	// (`a` and `d` in the pins pane).
	if m.cmd != nil {
		return m.cmdKey(k)
	}
	if c := m.confirm; c != nil {
		m.confirm = nil
		if k.String() == "y" {
			return c.yes()
		}
		return m.setStatus("cancelled", false)
	}
	if m.search != nil && m.search.open {
		return m.searchKey(k)
	}
	if m.pins != nil {
		return m.pinsKey(k)
	}
	if m.chans != nil {
		return m.chansKey(k)
	}
	if m.shared != nil {
		return m.sharedKey(k)
	}
	if m.picker != nil {
		return m.pickerKey(k)
	}
	if m.opener != nil {
		return m.openerKey(k)
	}
	if m.sched != nil {
		return m.schedKey(k)
	}
	if m.help != nil {
		return m.helpKey(k)
	}
	if m.insert {
		return m.insertKey(k)
	}
	if !m.booted {
		if k.String() == "q" {
			return m.quit()
		}
		return nil
	}

	switch k.String() {
	case "q":
		return m.quit()
	case "?":
		return m.openHelp()
	case "ctrl+k", " ":
		m.switcher = m.newSwitcher()
		return nil
	case "/":
		return m.openSearch()
	case "n":
		return m.searchStep(1)
	case "N":
		return m.searchStep(-1)
	case ":":
		return m.openCmd()
	case "P":
		return m.openPins()
	case "F":
		return m.openShared()
	case "tab":
		m.cycleFocus(1)
		return nil
	case "shift+tab":
		m.cycleFocus(-1)
		return nil
	case "i", "a":
		return m.startInsert()
	case "u":
		return m.nextUnread()
	case "r":
		return m.markSelectedRead()
	}

	switch m.focus {
	case fSidebar:
		return m.sidebarKey(k)
	case fMessages:
		if m.current == unreadsID {
			return m.unreadsKey(k)
		}
		return m.listKey(k, m.lists[m.current], false)
	case fThread:
		if m.thread == nil {
			m.focus = fMessages
			return nil
		}
		switch k.String() {
		case "esc", "h", "left":
			m.focus = fMessages
			return nil
		case "x":
			m.thread = nil
			m.focus = fMessages
			return nil
		}
		return m.listKey(k, &m.thread.list, true)
	}
	return nil
}

func (m *Model) cycleFocus(dir int) {
	n := 2
	if m.thread != nil {
		n = 3
	}
	m.focus = focus((int(m.focus) + dir + n) % n)
}

func (m *Model) sidebarKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		m.moveSide(1)
	case "k", "up":
		m.moveSide(-1)
	case "g", "home":
		m.sideCur = 0
		m.moveSide(0)
	case "G", "end":
		m.sideCur = len(m.side) - 1
		m.moveSide(0)
	case "ctrl+d":
		m.moveSide(10)
	case "ctrl+u":
		m.moveSide(-10)
	case "z":
		return m.toggleFold()
	case "enter", "l", "right":
		if m.sideCur >= len(m.side) {
			return nil
		}
		if it := m.side[m.sideCur]; it.header != "" {
			return m.toggleFold()
		} else if it.id != "" {
			return m.open(it.id)
		}
	}
	return nil
}

// moveSide moves the cursor by d, skipping section headers.
func (m *Model) moveSide(d int) {
	if len(m.side) == 0 {
		return
	}
	step := 1
	if d < 0 {
		step = -1
	}
	i := min(max(m.sideCur+d, 0), len(m.side)-1)
	for i >= 0 && i < len(m.side) && !m.side[i].selectable() {
		i += step
	}
	if i < 0 || i >= len(m.side) { // ran off the end; search back the other way
		i = min(max(m.sideCur+d, 0), len(m.side)-1)
		for i >= 0 && i < len(m.side) && !m.side[i].selectable() {
			i -= step
		}
	}
	if i >= 0 && i < len(m.side) {
		m.sideCur = i
	}
}

func (m *Model) listKey(k tea.KeyMsg, l *msgList, inThread bool) tea.Cmd {
	if l == nil {
		return nil
	}
	n := len(l.msgs)
	if n == 0 {
		if k.String() == "h" || k.String() == "left" {
			m.focus = fSidebar
		}
		return nil
	}
	sel := l.sel
	if sel < 0 || sel >= n {
		sel = n - 1
	}
	var cmd tea.Cmd
	switch k.String() {
	case "j", "down":
		sel++
	case "k", "up":
		if sel == 0 && l.hasMore && !inThread {
			cmd = m.loadHistory(m.current, true)
		}
		sel--
	case "ctrl+d":
		sel += 10
	case "ctrl+u":
		sel -= 10
	case "g", "home":
		sel = 0
	case "G", "end", "esc":
		sel = n - 1
	case "h", "left":
		if inThread {
			m.focus = fMessages
		} else {
			m.focus = fSidebar
		}
		return nil
	case "l", "right":
		if !inThread && m.thread != nil {
			m.focus = fThread
		}
		return nil
	case "enter", "t":
		if !inThread {
			return m.openThread(l.msgs[sel])
		}
	case "+":
		return m.openPicker(m.listConv(inThread), l.msgs[sel])
	case "c":
		return m.copyMessage(l.msgs[sel])
	case "e":
		return m.startEdit(m.listConv(inThread), l.msgs[sel], inThread)
	case "D":
		return m.askDelete(m.listConv(inThread), l.msgs[sel])
	case "p":
		return m.togglePin(m.listConv(inThread), l.msgs[sel])
	case "v":
		return m.openViewer(l, sel)
	case "o":
		return m.openFrom(l.msgs[sel])
	case "s":
		return m.saveFiles(l.msgs[sel].Files)
	case "U":
		if inThread {
			return m.setStatus("Slack's API can't mark thread replies unread", false)
		}
		l.sel = sel // stay here, so following new messages doesn't read it again
		return m.markUnread(m.current, l.msgs[sel])
	}
	sel = min(max(sel, 0), n-1)
	if sel == n-1 {
		l.sel = -1 // back at the bottom: follow new messages again
		if !inThread {
			cmd = tea.Batch(cmd, m.markRead(m.current))
		}
	} else {
		l.sel = sel
	}
	return cmd
}

// openThread shows the thread for parent; with no replies yet it starts one.
func (m *Model) openThread(parent backend.Message) tea.Cmd {
	ts := parent.TS
	if parent.ThreadTS != "" {
		ts = parent.ThreadTS
	}
	m.thread = &threadState{conv: m.current, ts: ts, list: msgList{sel: -1, loading: true, msgs: []backend.Message{parent}}}
	m.focus = fThread
	return tea.Batch(m.loadThreadCache(m.current, ts), m.loadReplies(m.current, ts))
}

func (m *Model) unreadsKey(k tea.KeyMsg) tea.Cmd {
	convs := m.unreadConvs()
	n := len(convs)
	switch k.String() {
	case "j", "down":
		m.unreads.sel++
	case "k", "up":
		m.unreads.sel--
	case "g", "home":
		m.unreads.sel = 0
	case "G", "end":
		m.unreads.sel = n - 1
	case "h", "left":
		m.focus = fSidebar
	case "enter", "l", "right":
		if n > 0 {
			return m.open(convs[min(max(m.unreads.sel, 0), n-1)].ID)
		}
	}
	m.unreads.sel = min(max(m.unreads.sel, 0), max(n-1, 0))
	return nil
}

// markSelectedRead marks the conversation under the cursor as read.
func (m *Model) markSelectedRead() tea.Cmd {
	id := m.current
	switch {
	case m.focus == fSidebar && m.sideCur < len(m.side):
		id = m.side[m.sideCur].id
	case m.current == unreadsID:
		convs := m.unreadConvs()
		if len(convs) == 0 {
			return nil
		}
		id = convs[min(max(m.unreads.sel, 0), len(convs)-1)].ID
	}
	if id == unreadsID || id == "" {
		return nil
	}
	cmd := m.markRead(id)
	m.rebuildSidebar()
	return cmd
}

// markUnread moves the read marker to just before msg, like Slack's
// "Mark unread": the conversation is unread from msg on, until it is
// opened again or read to the bottom.
func (m *Model) markUnread(id string, msg backend.Message) tea.Cmd {
	c := m.convs[id]
	if c == nil {
		return nil
	}
	ts := backend.TSBefore(msg.TS)
	c.LastRead = ts
	if backend.TSLess(c.Latest, msg.TS) {
		c.Latest = msg.TS
	}
	delete(m.unreadMsgs, id) // the Unreads view fetches from the new marker
	m.markSeq[id]++          // and a pending mark-read tick is void
	m.list(id).newSince = ts
	m.rebuildSidebar()
	return tea.Batch(m.mark(id, ts), m.setStatus("marked unread from here", false))
}

// nextUnread opens the next unread conversation in sidebar order.
func (m *Model) nextUnread() tea.Cmd {
	n := len(m.side)
	for d := 1; d <= n; d++ {
		it := m.side[(m.sideCur+d)%n]
		if c := m.convs[it.id]; c != nil && c.Unread() && it.id != m.current {
			return m.open(it.id)
		}
	}
	return m.setStatus("no unread conversations", false)
}

// --- insert mode -----------------------------------------------------------

func (m *Model) inputTarget() (conv, threadTS string) {
	if m.focus == fThread && m.thread != nil {
		return m.thread.conv, m.thread.ts
	}
	if m.current == unreadsID {
		return "", ""
	}
	return m.current, ""
}

func (m *Model) startInsert() tea.Cmd {
	conv, _ := m.inputTarget()
	if conv == "" {
		return m.setStatus("open a conversation first", false)
	}
	if m.focus == fSidebar {
		m.focus = fMessages
	}
	m.insert = true
	return m.input.Focus()
}

func (m *Model) insertKey(k tea.KeyMsg) tea.Cmd {
	if m.complete != nil {
		if cmd, ok := m.completionKey(k); ok {
			return cmd
		}
	}
	switch {
	case k.Paste && m.editing == nil:
		return m.paste(string(k.Runes), false)
	case k.Type == tea.KeyCtrlV && m.editing == nil:
		return m.paste("", true)
	case k.Type == tea.KeyCtrlS && m.editing == nil:
		return m.laterFromInsert()
	case k.Type == tea.KeyCtrlO && m.editing == nil:
		if _, thread := m.inputTarget(); thread == "" {
			return m.setStatus("ctrl+o is for thread replies: also send to the channel", false)
		}
		m.broadcast = !m.broadcast
		return nil
	case k.Type == tea.KeyCtrlB:
		m.toggleMark('*')
		return nil
	case k.Alt && k.Type == tea.KeyRunes && string(k.Runes) == "i":
		m.toggleMark('_')
		return nil
	case k.Type == tea.KeyCtrlX && len(m.attach) > 0:
		m.attach = m.attach[:len(m.attach)-1]
		return m.setStatus(plural(len(m.attach), "1 image left", fmt.Sprintf("%d images left", len(m.attach))), false)
	case k.Type == tea.KeyEsc:
		if m.editing != nil {
			m.cancelEdit()
		}
		m.insert = false
		m.input.Blur()
		return nil
	case k.Type == tea.KeyEnter && !k.Alt && m.editing != nil:
		return m.saveEdit()
	case k.Type == tea.KeyUp && m.editing == nil && m.input.Value() == "":
		return m.editLast()
	case k.Type == tea.KeyEnter && k.Alt, k.Type == tea.KeyCtrlJ:
		m.input.InsertString("\n")
		m.fitInput()
		return nil
	case k.Type == tea.KeyEnter:
		text := strings.TrimSpace(m.input.Value())
		if text == "" && len(m.attach) == 0 {
			return nil
		}
		conv, thread := m.inputTarget()
		if conv == "" {
			return nil
		}
		m.input.Reset()
		m.fitInput()
		m.complete = nil
		if l := m.lists[conv]; l != nil && thread == "" {
			l.sel = -1
		}
		broadcast := m.broadcast && thread != ""
		m.broadcast = false
		if atts := m.attach; len(atts) > 0 {
			m.attach = nil
			status := plural(len(atts), "uploading 1 image…", fmt.Sprintf("uploading %d images…", len(atts)))
			if broadcast {
				status += " (only in the thread: Slack can't send files to the channel too)"
			}
			return tea.Batch(m.upload(conv, thread, atts, render.Encode(text, m.composeTokens)), m.setStatus(status, false))
		}
		return m.post(conv, thread, render.Encode(text, m.composeTokens), broadcast)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	m.fitInput()
	m.updateCompletion()
	return cmd
}

// fitInput grows the input box with its content, wrapped lines included,
// up to 8 rows or a third of the window, whichever is less; past that the
// box scrolls.
func (m *Model) fitInput() {
	limit := max(1, min(8, m.height/3))
	m.input.SetHeight(min(max(m.inputRows(), 1), limit))
}

// listConv is the conversation a message list belongs to.
func (m *Model) listConv(inThread bool) string {
	if inThread && m.thread != nil {
		return m.thread.conv
	}
	return m.current
}

// quit frees our images first. That has to happen now, while the TUI is
// still on the terminal's alternate screen where the images live; once the
// program exits it has switched back and the deletes would miss them.
func (m *Model) quit() tea.Cmd {
	if m.gfx != nil {
		m.gfx.Close()
	}
	return tea.Quit
}
