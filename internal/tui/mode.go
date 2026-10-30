package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/config"
)

// Modes ([[mode]] in the config) are duties turned on with `:<name>`, such
// as being the day's first responder. One is on at a time, until its end
// the next day or until turned off. While it is on, a new message in one of
// its watched channels turns the panes red and opens the channel, unless
// something is being typed; then `!` goes there.

type modeState struct {
	cfg   config.Mode
	until time.Time
}

// alertState is a new message in a watched channel that hasn't been looked at.
type alertState struct{ conv string }

// modeTick checks whether the mode has run out; seq tells a stale one.
type modeTick struct{ seq int }

func (m *Model) modeConfig(name string) (config.Mode, bool) {
	for _, md := range m.cfg.Modes {
		if md.Name != "" && strings.EqualFold(md.Name, name) {
			return md, true
		}
	}
	return config.Mode{}, false
}

// modeEnd is when a mode turned on at now ends: at until ("08:00";
// midnight when empty) the next day.
func modeEnd(until string, now time.Time) (time.Time, error) {
	var h, min int
	if until != "" {
		t, err := time.Parse("15:04", until)
		if err != nil {
			return time.Time{}, fmt.Errorf("until %q: want a time of day like 08:00", until)
		}
		h, min = t.Hour(), t.Minute()
	}
	y, mo, d := now.Date()
	return time.Date(y, mo, d+1, h, min, 0, 0, now.Location()), nil
}

// cmdMode is `:mode`: alone it shows the mode that is on, or lists them;
// `off` ends it; a name turns that one on or off.
func (m *Model) cmdMode(args string) tea.Cmd {
	args = strings.TrimSpace(args)
	switch strings.ToLower(args) {
	case "":
		if md := m.mode; md != nil {
			return m.setStatus(fmt.Sprintf("%s until %s (:%s again or :mode off ends it)",
				md.cfg.Name, clock(md.until, m.cfg.TimeFormat), md.cfg.Name), false)
		}
		if len(m.cfg.Modes) == 0 {
			return m.setStatus("no modes; add [[mode]] to config.toml", false)
		}
		var names []string
		for _, md := range m.cfg.Modes {
			names = append(names, ":"+md.Name)
		}
		return m.setStatus("modes: "+strings.Join(names, " "), false)
	case "off", "end", "stop":
		if m.mode == nil {
			return m.setStatus("no mode is on", false)
		}
		return m.endMode(true)
	}
	md, ok := m.modeConfig(args)
	if !ok {
		return m.setStatus("no mode "+args+" in config.toml", true)
	}
	return m.toggleMode(md)
}

// toggleMode turns md off when it is on, otherwise on in place of any other.
func (m *Model) toggleMode(md config.Mode) tea.Cmd {
	if m.mode != nil && strings.EqualFold(m.mode.cfg.Name, md.Name) {
		return m.endMode(true)
	}
	until, err := modeEnd(md.Until, time.Now())
	if err != nil {
		return m.setStatus("mode "+md.Name+": "+err.Error(), true)
	}
	var cmds []tea.Cmd
	if m.mode != nil {
		cmds = append(cmds, m.endMode(true))
	}
	m.mode = &modeState{cfg: md, until: until}
	m.modeSeq++
	note := md.Name + " on until " + clock(until, m.cfg.TimeFormat)
	var missing []string
	for _, w := range md.Watch {
		if m.convByName(strings.TrimPrefix(w, "#")) == nil {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		note += "; not in " + strings.Join(missing, ", ")
	}
	cmds = append(cmds, m.saveMode(), m.modeLater(), m.setStatus(note, len(missing) > 0))
	if emoji, text := modeStatus(md); emoji != "" || text != "" {
		cmds = append(cmds, m.sendStatusNote(emoji, text, until, note))
	}
	return tea.Sequence(cmds...)
}

// endMode turns the mode off; clearStatus also clears the status it set,
// unless it has been changed since.
func (m *Model) endMode(clearStatus bool) tea.Cmd {
	md := m.mode
	m.mode, m.alert = nil, nil
	m.modeSeq++
	note := md.cfg.Name + " off"
	cmds := []tea.Cmd{m.saveMode(), m.setStatus(note, false)}
	if emoji, text := modeStatus(md.cfg); clearStatus && (emoji != "" || text != "") {
		me := m.users[m.client.SelfID]
		if me.StatusEmoji == emoji && me.StatusText == text {
			cmds = append(cmds, m.sendStatusNote("", "", time.Time{}, note))
		}
	}
	return tea.Sequence(cmds...)
}

// modeStatus is the status a mode sets, the emoji in its :colon: form.
func modeStatus(md config.Mode) (emoji, text string) {
	if e := strings.Trim(md.Emoji, ":"); e != "" {
		emoji = ":" + e + ":"
	}
	return emoji, md.Text
}

// restoreMode turns back on the mode that was on when the client last ran,
// if it hasn't run out.
func (m *Model) restoreMode(st config.State) tea.Cmd {
	if st.Mode == "" {
		return nil
	}
	md, ok := m.modeConfig(st.Mode)
	until := time.Unix(st.ModeUntil, 0)
	if !ok || !time.Now().Before(until) {
		return m.saveMode()
	}
	m.mode = &modeState{cfg: md, until: until}
	m.modeSeq++
	return m.modeLater()
}

func (m *Model) saveMode() tea.Cmd {
	st := config.LoadState()
	st.Mode, st.ModeUntil = "", 0
	if m.mode != nil {
		st.Mode, st.ModeUntil = m.mode.cfg.Name, m.mode.until.Unix()
	}
	if err := config.SaveState(st); err != nil {
		return m.setStatus("saving mode: "+err.Error(), true)
	}
	return nil
}

// modeLater checks every minute, by the wall clock, so a laptop waking up
// the next day ends the mode.
func (m *Model) modeLater() tea.Cmd {
	seq := m.modeSeq
	return tea.Tick(time.Minute, func(time.Time) tea.Msg { return modeTick{seq: seq} })
}

func (m *Model) onModeTick(t modeTick) tea.Cmd {
	if m.mode == nil || t.seq != m.modeSeq {
		return nil
	}
	if time.Now().Before(m.mode.until) {
		return m.modeLater()
	}
	// Slack clears the status at the same time by itself.
	return m.endMode(false)
}

func (m *Model) watched(convID string) bool {
	c := m.convs[convID]
	if m.mode == nil || c == nil || c.IsDM() {
		return false
	}
	for _, w := range m.mode.cfg.Watch {
		if strings.EqualFold(strings.TrimPrefix(w, "#"), c.Name) {
			return true
		}
	}
	return false
}

// onWatched raises the alert for a new message in a watched channel, and
// opens the channel when nothing is being typed.
func (m *Model) onWatched(ev backend.EvMessage) tea.Cmd {
	msg := ev.Msg
	if !m.watched(ev.Channel) || msg.IsReply() || msg.User == m.client.SelfID {
		return nil
	}
	switch msg.SubType {
	case "", "bot_message", "file_share":
	default: // joins, topic changes, thread broadcasts
		return nil
	}
	m.alert = &alertState{conv: ev.Channel}
	if m.busy() {
		return nil
	}
	return m.open(ev.Channel)
}

// busy is whether a jump would get in the way: something being typed or a
// pane open over the conversation.
func (m *Model) busy() bool {
	return m.insert || m.cmd != nil || m.switcher != nil || m.confirm != nil || m.viewer != nil || m.overlayOpen()
}

// seeAlert drops the alert once a key is pressed while its channel is shown.
func (m *Model) seeAlert() {
	if a := m.alert; a != nil && m.current == a.conv && !m.busy() {
		m.alert = nil
	}
}

// jumpAlert is `!`: the channel with the alert, or else the mode's first
// watched channel.
func (m *Model) jumpAlert() tea.Cmd {
	if a := m.alert; a != nil {
		m.alert = nil
		return m.open(a.conv)
	}
	if m.mode != nil {
		for _, w := range m.mode.cfg.Watch {
			if c := m.convByName(strings.TrimPrefix(w, "#")); c != nil {
				return m.open(c.ID)
			}
		}
	}
	return m.setStatus("no alert", false)
}

// modeHelp describes a mode for the suggestions and the help.
func (m *Model) modeHelp(md config.Mode) string {
	d := "mode: on / off"
	if len(md.Watch) > 0 {
		d += ", watches " + strings.Join(md.Watch, " ")
	}
	until := md.Until
	if until == "" {
		until = "midnight"
	}
	return d + "; ends " + until + " the next day"
}

// modeBadge is the status bar's mark of the mode: its name, or on an alert
// the channel in the alert colour.
func (m *Model) modeBadge() string {
	if m.mode == nil {
		return ""
	}
	if a := m.alert; a != nil {
		label := "⚠ " + m.mode.cfg.Name
		if c := m.convs[a.conv]; c != nil {
			label = "⚠ #" + c.Name
		}
		if m.current != a.conv {
			label += " · ! go"
		}
		return sModeAlert.Render(label)
	}
	return sDuty.Render("⚑ " + m.mode.cfg.Name)
}
