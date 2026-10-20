package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

// New conversations: `:dm @anna [@bob…]` opens the DM or group DM with
// them, and ctrl+k lists people we have no DM with yet.

// personPrefix marks a switcher entry that is a person, not a conversation.
const personPrefix = "\x00person:"

type dmOpenedMsg struct {
	conv  backend.Conversation
	names string
	err   error
}

func (m *Model) cmdDM(args string) tea.Cmd {
	var ids []string
	for _, w := range strings.Fields(args) {
		u, err := m.userByHandle(w)
		if err != nil && u.ID != m.client.SelfID {
			return m.setStatus(err.Error(), true)
		}
		if !slices.Contains(ids, u.ID) {
			ids = append(ids, u.ID)
		}
	}
	if len(ids) > 1 { // you are in every group DM anyway
		ids = slices.DeleteFunc(ids, func(id string) bool { return id == m.client.SelfID })
	}
	if len(ids) == 0 {
		return m.setStatus("usage: :dm @person [@person…]", false)
	}
	return m.openDMWith(ids)
}

// openDMWith opens the conversation with exactly these people, starting it
// when there is none yet.
func (m *Model) openDMWith(ids []string) tea.Cmd {
	if c := m.findDM(ids); c != nil {
		return m.open(c.ID)
	}
	var names []string
	for _, id := range ids {
		names = append(names, m.UserName(id))
	}
	who := strings.Join(names, ", ")
	return tea.Batch(m.setStatus("opening "+who+"…", false), func() tea.Msg {
		c, err := m.client.OpenConversation(m.ctx, ids)
		return dmOpenedMsg{conv: c, names: who, err: err}
	})
}

// findDM finds the DM we already have with one person, or the group DM
// with exactly these people.
func (m *Model) findDM(ids []string) *backend.Conversation {
	want := map[string]bool{m.users[m.client.SelfID].Name: true}
	for _, id := range ids {
		want[m.users[id].Name] = true
	}
	for _, c := range m.convs {
		switch {
		case len(ids) == 1 && c.Kind == backend.KindDM && c.UserID == ids[0]:
			return c
		case len(ids) > 1 && c.Kind == backend.KindGroup:
			got := groupUsernames(c)
			if len(got) == len(want) && !slices.ContainsFunc(got, func(n string) bool { return !want[n] }) {
				return c
			}
		}
	}
	return nil
}

// groupUsernames reads the members of a group DM from its name:
// mpdm-anna--bob--carl-1 → anna, bob, carl.
func groupUsernames(c *backend.Conversation) []string {
	raw := strings.TrimSuffix(strings.TrimPrefix(c.Name, "mpdm-"), "-1")
	return strings.Split(raw, "--")
}

func (m *Model) dmOpenedResult(msg dmOpenedMsg) tea.Cmd {
	if msg.err != nil {
		return m.setStatus("couldn't open a conversation with "+msg.names+": "+slackReason(msg.err), true)
	}
	m.addConv(msg.conv)
	return tea.Batch(m.open(msg.conv.ID), m.presenceFor(msg.conv.ID), m.setStatus("", false))
}

// switcherPeople adds everyone we have no DM with to the ctrl+k
// candidates. They only show once something is typed.
func (m *Model) switcherPeople(s *switcher) {
	hasDM := map[string]bool{}
	for _, c := range m.convs {
		if c.Kind == backend.KindDM {
			hasDM[c.UserID] = true
		}
	}
	var people []backend.User
	for _, u := range m.users {
		if !u.Deleted && !u.IsBot && u.ID != "USLACKBOT" && !hasDM[u.ID] {
			people = append(people, u)
		}
	}
	slices.SortFunc(people, func(a, b backend.User) int { return strings.Compare(a.Label(), b.Label()) })
	s.convCount = len(s.ids)
	for _, u := range people {
		label := u.Label()
		if u.RealName != "" && u.RealName != label {
			label += " " + u.RealName
		}
		s.ids = append(s.ids, personPrefix+u.ID)
		s.labels = append(s.labels, label)
	}
}
