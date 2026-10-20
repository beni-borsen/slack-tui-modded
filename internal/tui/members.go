package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// `:members`: who is in the open conversation, filtered as you type;
// enter shows the person's profile.

type membersState struct {
	conv     string
	ids      []string // nil while loading
	input    textinput.Model
	matches  []int // indexes into ids, best first
	sel, top int
}

type membersMsg struct {
	conv string
	ids  []string
	err  error
}

func (m *Model) openMembers() tea.Cmd {
	c := m.convs[m.current]
	if c == nil {
		return m.setStatus("open a conversation first", false)
	}
	ti := textinput.New()
	ti.Prompt = "› "
	ti.Placeholder = "filter people"
	ti.CharLimit = 80
	m.closeSearch()
	m.pins, m.sched, m.shared, m.chans = nil, nil, nil, nil
	m.insert = false
	m.input.Blur()
	m.members = &membersState{conv: c.ID, input: ti}
	id := c.ID
	return tea.Batch(m.members.input.Focus(), func() tea.Msg {
		ids, err := m.client.Members(m.ctx, id)
		return membersMsg{conv: id, ids: ids, err: err}
	})
}

func (m *Model) membersResult(msg membersMsg) tea.Cmd {
	s := m.members
	if s == nil || s.conv != msg.conv {
		return nil
	}
	if msg.err != nil {
		m.members = nil
		return m.setStatus("members: "+slackReason(msg.err), true)
	}
	// By name, as Slack lists them.
	slices.SortStableFunc(msg.ids, func(a, b string) int {
		return strings.Compare(strings.ToLower(m.memberName(a)), strings.ToLower(m.memberName(b)))
	})
	s.ids = msg.ids
	m.filterMembers()
	return nil
}

func (m *Model) memberName(id string) string {
	if u, ok := m.users[id]; ok {
		return u.Label()
	}
	return id
}

func (m *Model) filterMembers() {
	s := m.members
	q := strings.TrimPrefix(strings.TrimSpace(s.input.Value()), "@")
	s.sel, s.top = 0, 0
	s.matches = s.matches[:0]
	if q == "" {
		for i := range s.ids {
			s.matches = append(s.matches, i)
		}
		return
	}
	s.matches = rank(q, len(s.ids),
		func(i int) []string {
			u := m.users[s.ids[i]]
			return []string{u.Label(), u.RealName, u.Name, u.Title}
		},
		func(i int) int { return len(m.memberName(s.ids[i])) })
}

func (m *Model) membersKey(k tea.KeyMsg) tea.Cmd {
	s := m.members
	n := len(s.matches)
	page := max(1, m.height/2)
	switch k.String() {
	case "esc":
		m.members = nil
		return nil
	case "enter":
		if n == 0 {
			return nil
		}
		return m.openProfile(s.ids[s.matches[min(s.sel, n-1)]])
	case "down", "ctrl+n", "ctrl+j", "tab":
		s.sel++
	case "up", "ctrl+p", "shift+tab":
		s.sel--
	case "pgdown", "ctrl+d":
		s.sel += page
	case "pgup", "ctrl+u":
		s.sel -= page
	default:
		before := s.input.Value()
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(k)
		if s.input.Value() != before {
			m.filterMembers()
		}
		return cmd
	}
	s.sel = min(max(s.sel, 0), max(n-1, 0))
	return nil
}

func (m *Model) renderMembers(w, h int) string {
	s := m.members
	s.input.Width = max(10, w-6)
	title := sTitle.Render("Members")
	if c := m.convs[s.conv]; c != nil {
		title += sDim.Render("  " + m.convRef(c))
	}
	if s.ids != nil {
		title += sDim.Render(fmt.Sprintf("  %d %s", len(s.ids), plural(len(s.ids), "person", "people")))
	}
	header := []string{title, rule(w, true), " " + s.input.View(), ""}
	footer := sFaint.Render("  ") + sKey.Render("enter") + sFaint.Render(" profile  ") +
		sKey.Render("↑ ↓") + sFaint.Render(" move  ") + sKey.Render("esc") + sFaint.Render(" close")
	bodyH := max(1, h-len(header)-2)
	if len(s.matches) == 0 {
		msg := "nobody matches"
		if s.ids == nil {
			msg = "loading…"
		}
		return strings.Join(append(header, placeholder(msg, w, bodyH), rule(w, false), footer), "\n")
	}

	sel := min(max(s.sel, 0), len(s.matches)-1)
	if sel < s.top {
		s.top = sel
	}
	if sel >= s.top+bodyH {
		s.top = sel - bodyH + 1
	}
	var rows []string
	for i := s.top; i < len(s.matches) && len(rows) < bodyH; i++ {
		id := s.ids[s.matches[i]]
		u := m.users[id]
		name := m.memberName(id)
		if id == m.client.SelfID {
			name += " (you)"
		}
		if m.presence[id] {
			name += " " + sOK.Render(onlineDot)
		}
		var info []string
		if u.RealName != "" && u.RealName != u.Label() {
			info = append(info, u.RealName)
		}
		if u.Title != "" {
			info = append(info, u.Title)
		}
		if m.extUsers[id] {
			info = append(info, "external")
		}
		if u.IsBot {
			info = append(info, "app")
		}
		var row string
		if i == sel {
			row = sBar.Render("▌") + " " + sActive.Render(ansi.Strip(name))
		} else {
			row = "  " + sNormal.Render(name)
		}
		if line := m.statusLine(id); line != "" {
			row += " " + line
		}
		if len(info) > 0 {
			row += "  " + sDim.Render(strings.Join(info, " · "))
		}
		rows = append(rows, ansi.Truncate(row, w, "…"))
	}
	for len(rows) < bodyH {
		rows = append(rows, "")
	}
	return strings.Join(append(append(header, rows...), rule(w, false), footer), "\n")
}

// memberIDs are people in the open members list that users.list didn't
// have (other organisations), for scanUsers to look up.
func (m *Model) memberIDs() []string {
	if m.members == nil {
		return nil
	}
	return m.members.ids
}
