package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

type botMsg struct {
	id, name string
	err      error
}

// scanBots looks up the names of bots on screen that sent only their ID.
func (m *Model) scanBots() tea.Cmd {
	if m.bots == nil {
		m.bots = map[string]string{}
	}
	var cmds []tea.Cmd
	want := func(msgs []backend.Message) {
		for _, msg := range msgs {
			if id := msg.BotID; id != "" && msg.BotName == "" && msg.User == "" {
				if _, seen := m.bots[id]; !seen {
					m.bots[id] = ""
					cmds = append(cmds, m.fetchBot(id))
				}
			}
		}
	}
	if l := m.lists[m.current]; l != nil && m.current != unreadsID {
		want(l.msgs)
	}
	if m.thread != nil {
		want(m.thread.list.msgs)
	}
	if m.current == unreadsID {
		for _, msgs := range m.unreadMsgs {
			want(msgs)
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) fetchBot(id string) tea.Cmd {
	return func() tea.Msg {
		name, err := m.client.BotName(m.ctx, id)
		return botMsg{id: id, name: name, err: err}
	}
}
