package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/config"
)

// sideItem is a sidebar row: a conversation (id set), a section header
// (header set), or a blank spacer between sections.
type sideItem struct {
	header string
	id     string
	spacer bool
	hidden int // for a folded header: how many conversations it hides
}

func (it sideItem) key() string {
	if it.header != "" {
		return "section:" + it.header
	}
	return it.id
}

// selectable is true for rows the cursor can stop on.
func (it sideItem) selectable() bool { return !it.spacer }

type section struct {
	name string
	ids  []string
}

// sections groups every conversation: custom sections from the config
// first, then Channels, Direct messages and External (Slack Connect).
func (m *Model) sections() []section {
	var out []section
	used := map[string]bool{}
	for _, sec := range m.cfg.Sections {
		var ids []string
		for _, ref := range sec.Items {
			if id := m.resolveRef(ref); id != "" && !used[id] {
				used[id] = true
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			out = append(out, section{sec.Name, ids})
		}
	}

	var chans, dms, ext []*backend.Conversation
	for id, c := range m.convs {
		if used[id] {
			continue
		}
		if c.IsDM() {
			if c.Kind == backend.KindDM && m.users[c.UserID].Deleted {
				continue
			}
			if c.InfoLoaded && c.Latest == "" { // never had a message
				continue
			}
		}
		switch {
		case m.isExternal(c):
			ext = append(ext, c)
		case c.IsDM():
			dms = append(dms, c)
		default:
			chans = append(chans, c)
		}
	}
	slices.SortFunc(chans, func(a, b *backend.Conversation) int { return strings.Compare(a.Name, b.Name) })
	// DMs by recent activity, like Slack: one-to-one first, then groups.
	byGroupThenRecent := func(a, b *backend.Conversation) int {
		if ag, bg := a.Kind == backend.KindGroup, b.Kind == backend.KindGroup; ag != bg {
			if ag {
				return 1
			}
			return -1
		}
		return m.byRecent(a, b)
	}
	slices.SortFunc(dms, byGroupThenRecent)
	slices.SortFunc(ext, m.byRecent)
	ids := func(cs []*backend.Conversation) []string {
		out := make([]string, len(cs))
		for i, c := range cs {
			out[i] = c.ID
		}
		return out
	}
	if len(chans) > 0 {
		out = append(out, section{"Channels", ids(chans)})
	}
	if len(dms) > 0 {
		out = append(out, section{"Direct messages", ids(dms)})
	}
	if len(ext) > 0 {
		out = append(out, section{"External", ids(ext)})
	}
	return out
}

// byRecent orders conversations newest activity first.
func (m *Model) byRecent(a, b *backend.Conversation) int {
	if a.Latest != b.Latest {
		if backend.TSLess(a.Latest, b.Latest) {
			return 1
		}
		return -1
	}
	return strings.Compare(m.convLabel(a), m.convLabel(b))
}

// isExternal is true for Slack Connect: conversations shared with another
// organisation, and DMs with someone from one.
func (m *Model) isExternal(c *backend.Conversation) bool {
	if c.External {
		return true
	}
	if c.Kind != backend.KindDM {
		return false
	}
	return m.extUsers[c.UserID]
}

func (m *Model) rebuildSidebar() {
	keep := ""
	if m.sideCur < len(m.side) {
		keep = m.side[m.sideCur].key()
	}

	items := []sideItem{{id: unreadsID}}
	for _, sec := range m.sections() {
		items = append(items, sideItem{spacer: true})
		head := sideItem{header: sec.name}
		var rows []sideItem
		for _, id := range sec.ids {
			// A folded section still shows what needs attention.
			if m.folded[sec.name] {
				if c := m.convs[id]; c == nil || (!c.Unread() && id != m.current) {
					head.hidden++
					continue
				}
			}
			rows = append(rows, sideItem{id: id})
		}
		items = append(items, head)
		items = append(items, rows...)
	}

	m.side = items
	m.sideCur = 0
	for i, it := range items {
		if keep != "" && it.key() == keep {
			m.sideCur = i
		}
	}
}

// toggleFold folds or unfolds the section under the cursor (or the one
// the cursor's conversation is in) and remembers it.
func (m *Model) toggleFold() tea.Cmd {
	name := ""
	for i := m.sideCur; i >= 0 && i < len(m.side); i-- {
		if m.side[i].header != "" {
			name = m.side[i].header
			break
		}
	}
	if name == "" {
		return nil
	}
	m.folded[name] = !m.folded[name]
	m.rebuildSidebar()
	m.sideFindKey("section:" + name)

	st := config.LoadState()
	st.Folded = st.Folded[:0]
	for n, f := range m.folded {
		if f {
			st.Folded = append(st.Folded, n)
		}
	}
	slices.Sort(st.Folded)
	if err := config.SaveState(st); err != nil {
		return m.setStatus("saving fold state: "+err.Error(), true)
	}
	return nil
}

func (m *Model) sideFindKey(key string) {
	for i, it := range m.side {
		if it.key() == key {
			m.sideCur = i
			return
		}
	}
}

func (m *Model) unreadCount() int {
	n := 0
	for _, c := range m.convs {
		if c.Unread() {
			n++
		}
	}
	return n
}

func (m *Model) renderSidebar(width, height int) string {
	lines := []string{
		sTitle.Render(ansi.Truncate(m.client.TeamName, width-1, "…")),
		sFaint.Render(strings.Repeat("─", width-1)),
	}
	bodyH := height - len(lines)

	// Keep the cursor in view.
	if m.sideCur < m.sideTop {
		m.sideTop = m.sideCur
	}
	if m.sideCur >= m.sideTop+bodyH {
		m.sideTop = m.sideCur - bodyH + 1
	}
	m.sideTop = max(0, min(m.sideTop, len(m.side)-bodyH))

	for i := m.sideTop; i < len(m.side) && i < m.sideTop+bodyH; i++ {
		lines = append(lines, m.sideRow(m.side[i], i == m.sideCur, width-1))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) sideRow(it sideItem, cursor bool, width int) string {
	if it.spacer {
		return ""
	}
	if it.header != "" {
		marker := " "
		if cursor && m.focus == fSidebar {
			marker = sBar.Render("▌")
		}
		arrow := "▾"
		if m.folded[it.header] {
			arrow = "▸"
		}
		icon := m.sectionIcon(it.header)
		if icon != "" {
			icon += " "
		}
		room := width - 6 - ansi.StringWidth(icon)
		row := sHeader.Render(arrow+" ") + icon + sHeader.Render(ansi.Truncate(strings.ToUpper(it.header), room, "…"))
		if it.hidden > 0 {
			row += sFaint.Render(fmt.Sprintf(" %d", it.hidden))
		}
		return marker + row
	}

	var icon, label string
	unread, badge := false, 0
	if it.id == unreadsID {
		n := m.unreadCount()
		icon, label, unread = unreadsIcon, "Unreads", n > 0
		if n > 0 {
			label = fmt.Sprintf("Unreads (%d)", n)
		}
	} else {
		c := m.convs[it.id]
		if c == nil {
			return ""
		}
		switch c.Kind {
		case backend.KindChannel:
			icon = "#"
		case backend.KindPrivate:
			icon = lockIcon
		case backend.KindDM:
			icon = userIcon
		case backend.KindGroup:
			icon = groupIcon
		}
		label = m.convLabel(c)
		unread, badge = c.Unread(), c.Mentions
	}

	marker := " "
	if cursor && m.focus == fSidebar {
		marker = sBar.Render("▌")
	}
	badgeStr := ""
	if badge > 0 {
		badgeStr = sBadge.Render(fmt.Sprint(badge))
	}
	room := width - 4 - lipgloss.Width(badgeStr)
	label = ansi.Truncate(label, room, "…")

	style := sDim
	if unread {
		style = sUnread
	}
	text := style.Render(icon + " " + label)
	pad := max(0, width-1-lipgloss.Width(text)-lipgloss.Width(badgeStr))
	row := text + strings.Repeat(" ", pad) + badgeStr
	if it.id == m.current {
		row = sActive.Render(ansi.Strip(icon+" "+label)+strings.Repeat(" ", pad)) + badgeStr
	}
	return marker + row
}
