package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

// A person's profile card: `I` on a message (its sender) or a DM, or
// `:profile @person`. Title, local time, status, email; enter writes to them.

type profileState struct {
	id      string
	loading bool
}

type profileMsg struct {
	user backend.User
	err  error
}

// openProfile shows what we know of a person at once and fetches the rest:
// users.list can be a day old, and leaves out people from other
// organisations.
func (m *Model) openProfile(id string) tea.Cmd {
	if id == "" {
		return nil
	}
	m.profile = &profileState{id: id, loading: true}
	var cmd tea.Cmd
	if _, known := m.presence[id]; !known && id != m.client.SelfID && m.cfg.PresenceMinutes >= 0 {
		cmd = m.fetchPresence([]string{id}, false)
	}
	return tea.Batch(cmd, func() tea.Msg {
		u, err := m.client.UserInfo(m.ctx, id)
		return profileMsg{user: u, err: err}
	})
}

func (m *Model) profileResult(msg profileMsg) tea.Cmd {
	if p := m.profile; p != nil && (msg.err != nil || p.id == msg.user.ID) {
		p.loading = false
	}
	if msg.err != nil {
		return m.setStatus("profile: "+msg.err.Error(), true)
	}
	m.users[msg.user.ID] = msg.user
	return nil
}

// cmdProfile opens the profile of the person named, or of the one this
// DM is with.
func (m *Model) cmdProfile(arg string) tea.Cmd {
	if arg = strings.TrimSpace(arg); arg != "" {
		u, err := m.userByHandle(arg)
		if err != nil && u.ID != m.client.SelfID {
			return m.setStatus(err.Error(), true)
		}
		return m.openProfile(u.ID)
	}
	if c := m.convs[m.current]; c != nil && c.Kind == backend.KindDM {
		return m.openProfile(c.UserID)
	}
	return m.setStatus("usage: :profile @person (or I on a message)", false)
}

// profileFromSidebar is `I` in the sidebar: the person of the DM under
// the cursor.
func (m *Model) profileFromSidebar() tea.Cmd {
	if m.sideCur < len(m.side) {
		if c := m.convs[m.side[m.sideCur].id]; c != nil && c.Kind == backend.KindDM {
			return m.openProfile(c.UserID)
		}
	}
	return m.setStatus("I shows a person's profile: select a message or a DM", false)
}

// profileOfMessage is `I` on a message: its sender.
func (m *Model) profileOfMessage(msg backend.Message) tea.Cmd {
	if msg.User == "" {
		return m.setStatus("apps have no profile", false)
	}
	return m.openProfile(msg.User)
}

func (m *Model) profileKey(k tea.KeyMsg) tea.Cmd {
	id := m.profile.id
	switch k.String() {
	case "esc", "q", "I":
		m.profile = nil
	case "enter", "i":
		m.profile = nil
		return m.openDMWith([]string{id})
	case "c":
		return m.copyText(m.users[id].Email)
	}
	return nil
}

func (m *Model) renderProfile(width int) string {
	p := m.profile
	u := m.users[p.id]
	boxW := min(64, width-4)
	inner := boxW - 4

	name := u.RealName
	if name == "" {
		name = u.Label()
	}
	if name == "" {
		name = p.id
	}
	head := sTitle.Render(name)
	if active, ok := m.presence[p.id]; ok && active {
		head += " " + sOK.Render(onlineDot)
	}
	rows := []string{head}
	var sub []string
	if u.DisplayName != "" && u.DisplayName != name {
		sub = append(sub, u.DisplayName)
	}
	if u.Name != "" {
		sub = append(sub, "@"+u.Name)
	}
	if u.Pronouns != "" {
		sub = append(sub, u.Pronouns)
	}
	if len(sub) > 0 {
		rows = append(rows, sDim.Render(strings.Join(sub, " · ")))
	}
	if u.Title != "" {
		rows = append(rows, u.Title)
	}
	rows = append(rows, sFaint.Render(strings.Repeat("─", inner)))

	field := func(label, value string) {
		if value != "" {
			rows = append(rows, sDim.Render(fmt.Sprintf("%-11s", label))+value)
		}
	}
	if line := m.statusLine(p.id); line != "" {
		if exp := u.StatusExpires; exp > 0 {
			line += sDim.Render(" until " + clock(time.Unix(exp, 0), m.cfg.TimeFormat))
		}
		field("Status", line)
	}
	if active, ok := m.presence[p.id]; ok && p.id != m.client.SelfID {
		field("Presence", map[bool]string{true: sOK.Render("active"), false: "away"}[active])
	}
	field("Local time", localTime(u, time.Now(), m.cfg.TimeFormat))
	field("Email", u.Email)
	field("Phone", u.Phone)
	if m.extUsers[p.id] {
		field("Workspace", "another organisation (Slack Connect)")
	}
	if u.Deleted {
		field("Account", sErr.Render("deactivated"))
	}
	if p.loading {
		rows = append(rows, sFaint.Render("loading…"))
	}
	for i, r := range rows {
		rows[i] = ansi.Truncate(r, inner, "…")
	}
	foot := "enter message · esc close"
	if u.Email != "" {
		foot = "enter message · c copy email · esc close"
	}
	rows = append(rows, "", sFaint.Render(foot))
	return sBox.Width(boxW - 2).Render(strings.Join(rows, "\n"))
}

// theirClock is now in the person's time zone, if their profile has one.
func theirClock(u backend.User, now time.Time) (time.Time, bool) {
	if u.TZ == "" && u.TZOffset == 0 {
		return time.Time{}, false
	}
	loc, err := time.LoadLocation(u.TZ)
	if err != nil || u.TZ == "" {
		loc = time.FixedZone(u.TZLabel, u.TZOffset)
	}
	return now.In(loc), true
}

// localTime is the person's clock now, and how far it is from ours:
// "16:04  Central European Summer Time · 1 h ahead".
func localTime(u backend.User, now time.Time, layout string) string {
	there, ok := theirClock(u, now)
	if !ok {
		return ""
	}
	s := there.Format(layout)
	if u.TZLabel != "" {
		s += "  " + sDim.Render(u.TZLabel)
	}
	_, ours := now.Zone()
	_, theirs := there.Zone()
	if d := time.Duration(theirs-ours) * time.Second; d != 0 {
		way := "ahead"
		if d < 0 {
			d, way = -d, "behind"
		}
		gap := fmt.Sprintf("%d h", int(d.Hours()))
		if m := int(d.Minutes()) % 60; m != 0 {
			gap = fmt.Sprintf("%d h %d min", int(d.Hours()), m)
			if d < time.Hour {
				gap = fmt.Sprintf("%d min", m)
			}
		}
		s += sDim.Render(" · " + gap + " " + way)
	}
	return s
}
