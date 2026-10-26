package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// editState is a message being edited in the input box.
type editState struct {
	conv     string
	msg      backend.Message
	tokens   map[string]string // @Name/#channel → Slack entity, from render.Editable
	inThread bool
}

// confirmState is a y/n question in the status bar.
type confirmState struct {
	prompt string
	yes    func() tea.Cmd
}

type editedMsg struct {
	conv string
	old  backend.Message // put back if the edit fails
	err  error
}

type deletedMsg struct {
	conv string
	ts   string
	err  error
}

// startEdit puts one of our messages into the input box.
func (m *Model) startEdit(conv string, msg backend.Message, inThread bool) tea.Cmd {
	if msg.User != m.client.SelfID {
		return m.setStatus("you can only edit your own messages", false)
	}
	if systemSubtypes[msg.SubType] {
		return m.setStatus("that message can't be edited", false)
	}
	text, tokens := render.Editable(msg.Text, m)
	m.editing = &editState{conv: conv, msg: msg, tokens: tokens, inThread: inThread}
	if inThread {
		m.focus = fThread
	} else {
		m.focus = fMessages
	}
	m.input.SetValue(text)
	m.input.CursorEnd()
	m.fitInput()
	m.insert = true
	return m.input.Focus()
}

// editLast edits our newest message in the pane being typed into, like ↑
// in Slack's empty composer.
func (m *Model) editLast() tea.Cmd {
	conv, thread := m.inputTarget()
	var msgs []backend.Message
	inThread := thread != ""
	if inThread {
		msgs = m.thread.list.msgs
	} else if l := m.lists[conv]; l != nil {
		msgs = l.msgs
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].User == m.client.SelfID && !systemSubtypes[msgs[i].SubType] {
			return m.startEdit(conv, msgs[i], inThread)
		}
	}
	return m.setStatus("no message of yours to edit here", false)
}

func (m *Model) cancelEdit() {
	m.editing = nil
	m.complete = nil
	m.input.Reset()
	m.fitInput()
}

// saveEdit sends the edited text. The change shows at once and is put back
// if Slack refuses it.
func (m *Model) saveEdit() tea.Cmd {
	e := m.editing
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m.setStatus("a message can't be empty — D deletes it", false)
	}
	newText := render.Encode(text, e.tokens)
	m.cancelEdit()
	m.insert = false
	m.input.Blur()
	if newText == e.msg.Text {
		return nil
	}
	updated := e.msg
	updated.Text, updated.Edited = newText, true
	m.editMessage(e.conv, updated)
	old := e.msg
	return func() tea.Msg {
		return editedMsg{conv: e.conv, old: old, err: m.client.Edit(m.ctx, e.conv, old.TS, newText)}
	}
}

// askDelete asks before deleting one of our messages.
func (m *Model) askDelete(conv string, msg backend.Message) tea.Cmd {
	if msg.User != m.client.SelfID {
		return m.setStatus("you can only delete your own messages", false)
	}
	m.confirm = &confirmState{
		prompt: "delete this message? y/n",
		yes: func() tea.Cmd {
			return func() tea.Msg {
				return deletedMsg{conv: conv, ts: msg.TS, err: m.client.Delete(m.ctx, conv, msg.TS)}
			}
		},
	}
	return nil
}

// removeMessage drops a message from every view that shows it.
func (m *Model) removeMessage(conv, ts string) {
	m.touch(conv)
	if l, ok := m.lists[conv]; ok {
		l.msgs = remove(l.msgs, ts)
		if l.sel >= len(l.msgs) {
			l.sel = -1
		}
	}
	if t := m.thread; t != nil && t.conv == conv {
		if t.ts == ts {
			m.thread = nil
			m.focus = fMessages
		} else {
			t.list.msgs = remove(t.list.msgs, ts)
		}
	}
	if u, ok := m.unreadMsgs[conv]; ok {
		m.unreadMsgs[conv] = remove(u, ts)
	}
}
