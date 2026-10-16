package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// switcher is the Ctrl-K fuzzy conversation finder.
type switcher struct {
	input   textinput.Model
	ids     []string // all candidates, in sidebar order
	labels  []string
	matches []string // ids matching the query, best first
	sel     int
}

func (m *Model) newSwitcher() *switcher {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.Placeholder = "jump to a channel or person"
	ti.Focus()
	s := &switcher{input: ti}
	// Every conversation, including those in folded sections.
	s.ids = append(s.ids, unreadsID)
	s.labels = append(s.labels, "Unreads")
	for _, sec := range m.sections() {
		for _, id := range sec.ids {
			c := m.convs[id]
			label := m.convLabel(c)
			if !c.IsDM() {
				label = "#" + label
			}
			s.ids = append(s.ids, id)
			s.labels = append(s.labels, label)
		}
	}
	s.filter()
	return s
}

func (s *switcher) filter() {
	q := strings.TrimSpace(s.input.Value())
	s.matches = s.matches[:0]
	if q == "" {
		s.matches = append(s.matches, s.ids...)
	} else {
		for _, i := range fuzzyRank(q, s.labels) {
			s.matches = append(s.matches, s.ids[i])
		}
	}
	s.sel = 0
}

func (m *Model) switcherKey(k tea.KeyMsg) tea.Cmd {
	s := m.switcher
	switch k.String() {
	case "esc", "ctrl+k":
		m.switcher = nil
		return nil
	case "enter":
		m.switcher = nil
		if len(s.matches) > 0 {
			return m.open(s.matches[s.sel])
		}
		return nil
	case "down", "ctrl+n", "ctrl+j", "tab":
		if s.sel < len(s.matches)-1 {
			s.sel++
		}
		return nil
	case "up", "ctrl+p", "shift+tab":
		if s.sel > 0 {
			s.sel--
		}
		return nil
	}
	var cmd tea.Cmd
	before := s.input.Value()
	s.input, cmd = s.input.Update(k)
	if s.input.Value() != before {
		s.filter()
	}
	return cmd
}

func (m *Model) renderSwitcher(width, height int) string {
	s := m.switcher
	boxW := min(60, width-4)
	inner := boxW - 4
	s.input.Width = inner - 3
	rows := []string{s.input.View(), sFaint.Render(strings.Repeat("─", inner))}
	listH := max(3, min(15, height-8))
	start := max(0, s.sel-listH+1)
	for i := start; i < len(s.matches) && i < start+listH; i++ {
		id := s.matches[i]
		label := "◆ Unreads"
		unread := false
		if c := m.convs[id]; c != nil {
			icon := "#"
			if c.IsDM() {
				icon = "•"
			}
			label = icon + " " + m.convLabel(c)
			unread = c.Unread()
		} else {
			unread = m.unreadCount() > 0
		}
		label = ansi.Truncate(label, inner-2, "…")
		style := sDim
		if unread {
			style = sUnread
		}
		if i == s.sel {
			rows = append(rows, sBar.Render("▌")+sActive.Render(" "+ansi.Strip(label)+strings.Repeat(" ", max(0, inner-2-ansi.StringWidth(label)))))
		} else {
			rows = append(rows, "  "+style.Render(label))
		}
	}
	if len(s.matches) == 0 {
		rows = append(rows, sDim.Render("  no matches"))
	}
	return sBox.Width(boxW - 2).Render(strings.Join(rows, "\n"))
}
