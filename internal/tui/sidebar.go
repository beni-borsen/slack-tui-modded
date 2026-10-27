package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/config"
)

// sideItem is a sidebar row: a conversation (id set), a section header
// (header set), a blank spacer between sections, the "… N more" row under a
// long DM list (more set), or the row that shows or hides a section's muted
// conversations (muted set).
type sideItem struct {
	header string
	id     string
	spacer bool
	hidden int    // for a folded header: how many conversations it hides
	more   int    // DMs left out of the list
	muted  int    // muted conversations in the section
	sec    string // the section of a more or muted row
}

func (it sideItem) key() string {
	switch {
	case it.header != "":
		return "section:" + it.header
	case it.more > 0:
		return "more:" + it.sec
	case it.muted > 0:
		return "muted:" + it.sec
	}
	return it.id
}

// selectable is true for rows the cursor can stop on.
func (it sideItem) selectable() bool { return !it.spacer }

// Built-in sections that need special rows.
const (
	secDMs  = "Direct messages"
	secApps = "Apps"
)

// dmShown is how many DMs the list shows before "… N more"; the rest are a
// ctrl+k away.
const dmShown = 10

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

	var chans, dms, apps, ext []*backend.Conversation
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
		case c.Kind == backend.KindDM && m.isApp(c.UserID):
			apps = append(apps, c)
		case c.IsDM():
			dms = append(dms, c)
		default:
			chans = append(chans, c)
		}
	}
	slices.SortFunc(chans, func(a, b *backend.Conversation) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(dms, m.byRecent)
	slices.SortFunc(apps, m.byRecent)
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
		out = append(out, section{secDMs, ids(dms)})
	}
	if len(apps) > 0 {
		out = append(out, section{secApps, ids(apps)})
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

// isApp is true for bots and Slack's own accounts (Slackbot), whose DMs go
// under Apps.
func (m *Model) isApp(userID string) bool {
	return userID == "USLACKBOT" || m.users[userID].IsBot
}

// needsAttention is an unread conversation that isn't muted, or a muted one
// that mentions the user.
func (m *Model) needsAttention(c *backend.Conversation) bool {
	return c.Unread() && (!m.muted[c.ID] || c.Mentions > 0)
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
		var rows, muted []sideItem
		more := 0
		for _, id := range sec.ids {
			c := m.convs[id]
			if c == nil {
				continue
			}
			// A folded section, a long DM list and the muted rows still
			// show what needs attention, and the open conversation.
			show := id == m.current || m.needsAttention(c)
			switch {
			case m.folded[sec.name] && !show:
				head.hidden++
			case m.muted[id] && !show:
				muted = append(muted, sideItem{id: id})
			case sec.name == secDMs && len(rows) >= dmShown && !show:
				more++
			default:
				rows = append(rows, sideItem{id: id})
			}
		}
		items = append(items, head)
		items = append(items, rows...)
		if more > 0 {
			items = append(items, sideItem{more: more, sec: sec.name})
		}
		if len(muted) > 0 {
			if m.showMuted {
				items = append(items, muted...)
			}
			items = append(items, sideItem{muted: len(muted), sec: sec.name})
		}
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
	st.Folded, st.Unfolded = st.Folded[:0], st.Unfolded[:0]
	for n, f := range m.folded {
		switch {
		case foldedByDefault[n] && !f:
			st.Unfolded = append(st.Unfolded, n)
		case !foldedByDefault[n] && f:
			st.Folded = append(st.Folded, n)
		}
	}
	slices.Sort(st.Folded)
	slices.Sort(st.Unfolded)
	if err := config.SaveState(st); err != nil {
		return m.setStatus("saving fold state: "+err.Error(), true)
	}
	return nil
}

// foldedByDefault are sections that start collapsed.
var foldedByDefault = map[string]bool{secApps: true}

// toggleMute mutes or unmutes the conversation under the sidebar cursor.
// Slack's API can neither read nor set mutes, so they are this client's own.
func (m *Model) toggleMute() tea.Cmd {
	if m.sideCur >= len(m.side) {
		return nil
	}
	id := m.side[m.sideCur].id
	c := m.convs[id]
	if c == nil {
		return nil
	}
	m.muted[id] = !m.muted[id]
	if !m.muted[id] {
		delete(m.muted, id)
	}
	m.rebuildSidebar()
	m.sideFindKey(id)

	st := config.LoadState()
	st.Muted = st.Muted[:0]
	for id := range m.muted {
		st.Muted = append(st.Muted, id)
	}
	slices.Sort(st.Muted)
	if err := config.SaveState(st); err != nil {
		return m.setStatus("saving mutes: "+err.Error(), true)
	}
	if m.muted[id] {
		return m.setStatus("muted "+m.convTitle(c), false)
	}
	return m.setStatus("unmuted "+m.convTitle(c), false)
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
		if m.needsAttention(c) {
			n++
		}
	}
	return n
}

func (m *Model) renderSidebar(width, height int) string {
	lines := []string{
		" " + sTitle.Render(ansi.Truncate(m.client.TeamName, width-1, "…")),
		sFaint.Render(strings.Repeat("─", width)),
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
		lines = append(lines, m.sideRow(m.side[i], i == m.sideCur, width))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// reHandle is a handle people put in their display name: "Kim 'kiky'".
var reHandle = regexp.MustCompile(`^(.+?) +'([^' ]+)'$`)

// sideRow draws one sidebar row in width cells: a cursor marker, a
// one-cell icon (the presence dot for people), the name, and on the right
// the status emoji and a mention badge or an activity dot.
func (m *Model) sideRow(it sideItem, cursor bool, width int) string {
	if it.spacer {
		return ""
	}
	marker := " "
	if cursor && m.focus == fSidebar {
		marker = sBar.Render("▌")
	}
	if it.header != "" {
		arrow := "▾"
		if m.folded[it.header] {
			arrow = "▸"
		}
		icon := m.sectionIcon(it.header)
		if icon != "" {
			icon += " "
		}
		var extra string
		switch {
		case it.hidden > 0:
			extra = sFaint.Render(fmt.Sprintf(" %d", it.hidden))
		case it.header == secDMs:
			extra = sFaint.Render(" · recent")
		}
		room := width - 3 - ansi.StringWidth(icon) - lipgloss.Width(extra)
		return marker + sHeader.Render(arrow+" ") + icon + sHeader.Render(ansi.Truncate(it.header, room, "…")) + extra
	}
	if it.more > 0 {
		return marker + "  " + sFaint.Render(fmt.Sprintf("… %d more  ", it.more)) + sBar.Render("ctrl+k")
	}
	if it.muted > 0 {
		sign := "+"
		if m.showMuted {
			sign = "−"
		}
		return marker + "  " + sFaint.Render(fmt.Sprintf("%s %d muted", sign, it.muted))
	}

	active := it.id == m.current
	// Every part takes the selection background on the open conversation.
	st := func(s lipgloss.Style) lipgloss.Style {
		if active {
			return s.Background(cSelBg)
		}
		return s
	}
	base := sDim
	var icon, name, extra, status, badge string
	iconStyle := sDim
	if it.id == unreadsID {
		n := m.unreadCount()
		icon, name = unreadsIcon, "Unreads"
		iconStyle = lipgloss.NewStyle().Foreground(cWarn)
		if n > 0 {
			base = sUnread
			badge = pill(sUnreadsBadge, fmt.Sprint(n), active)
		}
	} else {
		c := m.convs[it.id]
		if c == nil {
			return ""
		}
		name = m.convLabel(c)
		switch c.Kind {
		case backend.KindChannel:
			icon = "#"
		case backend.KindPrivate:
			icon = lockIcon
		case backend.KindDM:
			icon, iconStyle = awayDot, sFaint
			if m.presence[c.UserID] || (c.UserID == m.client.SelfID && !m.away) {
				icon, iconStyle = onlineDot, sPresent
			}
			if sm := reHandle.FindStringSubmatch(name); sm != nil {
				name, extra = sm[1], sm[2]
			}
			if s := m.statusEmoji(c.UserID); s != "" {
				status = s
			}
		case backend.KindGroup:
			icon, iconStyle = groupIcon, sGroup
			var more int
			name, more = m.groupShort(c)
			if more > 0 {
				extra = fmt.Sprintf("+%d", more)
			}
		}
		switch {
		case m.muted[c.ID]:
			base = sFaint
		case c.Unread():
			base = sUnread
			if c.Kind == backend.KindChannel || c.Kind == backend.KindPrivate {
				iconStyle = sDim
			}
		}
		switch {
		case c.Mentions > 0 && c.IsDM():
			badge = pill(sBadge, fmt.Sprint(c.Mentions), active)
		case c.Mentions > 0:
			badge = pill(sBadge, fmt.Sprintf("@%d", c.Mentions), active)
		case m.needsAttention(c):
			badge = sNormal.Render("•")
		}
		if active {
			base = sActive
		}
	}

	// The name gives way first, then its handle or +N.
	right := 0
	if status != "" {
		right += ansi.StringWidth(status) + 1
	}
	if badge != "" {
		right += lipgloss.Width(badge) + 1
	}
	room := max(1, width-1-2-right-1)
	if extra != "" && ansi.StringWidth(name)+1+ansi.StringWidth(extra) > room {
		if strings.HasPrefix(extra, "+") {
			name = ansi.Truncate(name, max(1, room-1-ansi.StringWidth(extra)), "…")
		} else {
			extra = ""
		}
	}
	name = ansi.Truncate(name, room, "…")
	text := st(iconStyle).Render(icon) + st(base).Render(" "+name)
	if extra != "" {
		text += st(base).Render(" ") + st(sDim.UnsetBold()).Render(extra)
	}
	tail := ""
	if status != "" {
		// The status emoji is drawn as is: a custom one is an image whose
		// placeholder cells must keep their own foreground colour, so only
		// the selection background goes around it.
		if active {
			status = selBgOn + status + selBgOff
		}
		tail += status + st(base).Render(" ")
	}
	if badge != "" {
		tail += badge + st(base).Render(" ")
	}
	pad := max(0, width-1-lipgloss.Width(text)-lipgloss.Width(tail))
	return marker + text + st(base).Render(strings.Repeat(" ", pad)) + tail
}

// pill draws a badge with rounded Nerd Font caps in place of its padding,
// so it keeps the same width. The caps sit on the row's own background.
func pill(s lipgloss.Style, text string, active bool) string {
	end := lipgloss.NewStyle().Foreground(s.GetBackground())
	if active {
		end = end.Background(cSelBg)
	}
	return end.Render(pillLeft) + s.UnsetPadding().Render(text) + end.Render(pillRight)
}

// groupShort names a group DM for the sidebar: the first two people other
// than the user and how many more there are. A named group keeps its name.
func (m *Model) groupShort(c *backend.Conversation) (string, int) {
	if !strings.HasPrefix(c.Name, "mpdm-") {
		return c.Name, 0
	}
	var names []string
	for _, uname := range groupUsernames(c) {
		if uname == m.users[m.client.SelfID].Name {
			continue
		}
		n := m.labelForUsername(uname)
		if sm := reHandle.FindStringSubmatch(n); sm != nil {
			n = sm[1]
		}
		names = append(names, n)
	}
	if len(names) <= 2 {
		return strings.Join(names, ", "), 0
	}
	return strings.Join(names[:2], ", "), len(names) - 2
}
