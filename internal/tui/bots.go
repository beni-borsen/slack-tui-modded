package tui

import (
	"regexp"

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

type userInfoMsg struct {
	user backend.User
	err  error
}

var reUserRef = regexp.MustCompile(`<@([UW][A-Z0-9]+)`)

// scanUsers looks up people that users.list left out (from other
// organisations, in shared channels and DMs), so they show by name, not ID.
func (m *Model) scanUsers() tea.Cmd {
	if !m.booted {
		return nil // users.list hasn't arrived yet; everyone looks unknown
	}
	if m.userLookups == nil {
		m.userLookups = map[string]bool{}
	}
	var cmds []tea.Cmd
	want := func(id string) {
		if id == "" || m.userLookups[id] {
			return
		}
		if _, known := m.users[id]; known {
			return
		}
		m.userLookups[id] = true
		cmds = append(cmds, m.fetchUser(id))
	}
	scan := func(msgs []backend.Message) {
		for _, msg := range msgs {
			want(msg.User)
			for _, ref := range reUserRef.FindAllStringSubmatch(msg.Text, -1) {
				want(ref[1])
			}
		}
	}
	for _, c := range m.convs {
		if c.Kind == backend.KindDM {
			want(c.UserID)
		}
	}
	for _, id := range m.memberIDs() {
		want(id)
	}
	if l := m.lists[m.current]; l != nil && m.current != unreadsID {
		scan(l.msgs)
	}
	if m.thread != nil {
		scan(m.thread.list.msgs)
	}
	if m.current == unreadsID {
		for _, msgs := range m.unreadMsgs {
			scan(msgs)
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) fetchUser(id string) tea.Cmd {
	return func() tea.Msg {
		u, err := m.client.UserInfo(m.ctx, id)
		return userInfoMsg{user: u, err: err}
	}
}
