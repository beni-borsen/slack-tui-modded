package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The `:` command line, vim style: it takes over the status bar, and its
// suggestions (commands, then emoji for :status) open above it.

type command struct {
	name string
	args string // usage, for the suggestions and the help
	help string
	run  func(m *Model, args string) tea.Cmd
}

var commands = []command{
	{"status", ":emoji: text [30m | until 17:00 | until tomorrow] | clear", "set your status; alone shows it",
		func(m *Model, a string) tea.Cmd { return m.cmdStatus(a) }},
	{"dnd", "30m | 2h | until 17:00 | until tomorrow | off", "pause notifications; alone shows the state",
		func(m *Model, a string) tea.Cmd { return m.cmdDND(a) }},
	{"later", "30m | 2h | 15:30 | 3pm | tomorrow [9:00] | fri [14]", "send the message being written later (ctrl+s while writing)",
		func(m *Model, a string) tea.Cmd { return m.cmdLater(a) }},
	{"scheduled", "", "scheduled messages: go to, back to composer, cancel",
		func(m *Model, _ string) tea.Cmd { return m.openScheduled() }},
	{"bookmark", "link [title]", "add a link to this conversation's bookmarks (a and e in P)",
		func(m *Model, a string) tea.Cmd { return m.cmdBookmark(a) }},
	{"pins", "", "pinned messages and bookmarks of this conversation (P)",
		func(m *Model, _ string) tea.Cmd { return m.openPins() }},
	{"files", "", "files and links shared in this conversation (F)",
		func(m *Model, _ string) tea.Cmd { return m.openShared() }},
	{"dm", "@person [@person…]", "message someone, or a group; starts the conversation if needed",
		func(m *Model, a string) tea.Cmd { return m.cmdDM(a) }},
	{"profile", "[@person]", "a person's profile: title, local time, email (I)",
		func(m *Model, a string) tea.Cmd { return m.cmdProfile(a) }},
	{"members", "", "who is in this conversation; enter shows a profile",
		func(m *Model, _ string) tea.Cmd { return m.openMembers() }},
	{"join", "[#channel]", "join a public channel; alone browses them all",
		func(m *Model, a string) tea.Cmd { return m.cmdJoin(a) }},
	{"leave", "[#channel]", "leave this channel (or the one named)",
		func(m *Model, a string) tea.Cmd { return m.cmdLeave(a) }},
	{"create", "[--private] name", "make a channel",
		func(m *Model, a string) tea.Cmd { return m.cmdCreate(a) }},
	{"invite", "@person [@person…]", "add people to this channel",
		func(m *Model, a string) tea.Cmd { return m.cmdInvite(a) }},
	{"away", "", "show as away",
		func(m *Model, _ string) tea.Cmd { return m.cmdAway(true) }},
	{"back", "", "show as active again (Slack decides)",
		func(m *Model, _ string) tea.Cmd { return m.cmdAway(false) }},
}

type cmdLine struct {
	input     textinput.Model
	complete  *completion
	dismissed string
	histAt    int // index into m.cmdHist while browsing it; len = the new line
	draft     string
	bookmark  *bookmarkEdit // `e` in the pins pane: :bookmark changes this one
}

func (m *Model) openCmd() tea.Cmd {
	ti := textinput.New()
	ti.Prompt = ":"
	ti.PromptStyle = sKey
	ti.CharLimit = 300
	m.cmd = &cmdLine{input: ti, histAt: len(m.cmdHist)}
	return m.cmd.input.Focus()
}

// findCommand matches a command by name or a prefix only it has (":st").
func findCommand(name string) (command, bool) {
	name = strings.ToLower(name)
	var hit []command
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
		if strings.HasPrefix(c.name, name) {
			hit = append(hit, c)
		}
	}
	if len(hit) == 1 && name != "" {
		return hit[0], true
	}
	return command{}, false
}

func (m *Model) runCmd(line string) tea.Cmd {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ":"))
	if line == "" {
		return nil
	}
	if n := len(m.cmdHist); n == 0 || m.cmdHist[n-1] != line {
		m.cmdHist = append(m.cmdHist, line)
	}
	name, args, _ := strings.Cut(line, " ")
	c, ok := findCommand(name)
	if !ok {
		return m.setStatus("unknown command :"+name+" (? lists them)", true)
	}
	return c.run(m, args)
}

func (m *Model) cmdKey(k tea.KeyMsg) tea.Cmd {
	c := m.cmd
	if c.complete != nil {
		switch k.String() {
		case "tab", "enter":
			m.acceptCmdCompletion()
			return nil
		case "down", "ctrl+n":
			c.complete.sel = min(c.complete.sel+1, len(c.complete.items)-1)
			return nil
		case "up", "ctrl+p":
			c.complete.sel = max(c.complete.sel-1, 0)
			return nil
		case "esc":
			c.dismissed = c.complete.trigger + c.complete.query
			c.complete = nil
			return nil
		}
	}
	switch k.String() {
	case "esc":
		m.cmd = nil
		return nil
	case "enter":
		line := c.input.Value()
		m.cmd = nil
		m.bmEdit = c.bookmark
		defer func() { m.bmEdit = nil }()
		return m.runCmd(line)
	case "backspace":
		if c.input.Value() == "" {
			m.cmd = nil // like vim: backspace past the colon leaves
			return nil
		}
	case "up", "down":
		m.browseCmdHist(k.String() == "up")
		return nil
	}
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(k)
	return tea.Batch(cmd, m.updateCmdCompletion())
}

func (m *Model) browseCmdHist(back bool) {
	c := m.cmd
	if c.histAt == len(m.cmdHist) {
		c.draft = c.input.Value()
	}
	if back {
		c.histAt = max(c.histAt-1, 0)
	} else {
		c.histAt = min(c.histAt+1, len(m.cmdHist))
	}
	if c.histAt == len(m.cmdHist) {
		c.input.SetValue(c.draft)
	} else {
		c.input.SetValue(m.cmdHist[c.histAt])
	}
	c.input.CursorEnd()
	c.complete = nil
}

// updateCmdCompletion suggests commands while the first word is typed, and
// after that channels or people (":join #de", ":invite @an") or emoji
// (":status :pal"). The command is a fetch the suggestions wait for.
func (m *Model) updateCmdCompletion() tea.Cmd {
	c := m.cmd
	before := []rune(c.input.Value())
	before = before[:min(c.input.Position(), len(before))]
	text := string(before)

	var cp *completion
	var load tea.Cmd
	if !strings.Contains(text, " ") {
		var items []compItem
		for _, cmd := range commands {
			if strings.HasPrefix(cmd.name, strings.ToLower(text)) && cmd.name != text {
				items = append(items, compItem{insert: cmd.name, label: cmd.name, detail: strings.TrimSpace(cmd.args + "  " + cmd.help)})
			}
		}
		if len(items) > 0 {
			cp = &completion{trigger: "cmd", query: text, items: items}
		}
	} else if arg, ok, cmd := m.argCompletion(text); ok {
		cp, load = arg, cmd
	} else if items := m.templateItems(text); items != nil {
		_, arg, _ := strings.Cut(text, " ")
		cp = &completion{trigger: "tpl", query: arg, items: items}
	} else if g := reEmojiQuery.FindStringSubmatch(strings.ToLower(text)); g != nil {
		var items []compItem
		for _, n := range m.matchEmoji(g[1]) {
			items = append(items, compItem{insert: ":" + n + ":", emoji: n})
		}
		if len(items) > 0 {
			cp = &completion{trigger: ":", query: g[1], items: items}
		}
	}
	if cp == nil || cp.trigger+cp.query == c.dismissed {
		c.complete = nil
		if cp == nil {
			c.dismissed = ""
		}
		return load
	}
	if old := c.complete; old != nil && old.trigger == cp.trigger && old.query == cp.query && len(old.items) == len(cp.items) {
		return load
	}
	c.complete = cp
	return load
}

func (m *Model) acceptCmdCompletion() {
	c := m.cmd
	cp := c.complete
	it := cp.items[cp.sel]
	v := []rune(c.input.Value())
	pos := min(c.input.Position(), len(v))
	n := len([]rune(cp.query))
	if cp.trigger == ":" {
		n++ // the colon
	}
	out := string(v[:pos-n]) + it.insert + " "
	c.input.SetValue(out + string(v[pos:]))
	c.input.SetCursor(len([]rune(out)))
	c.complete = nil
}

// renderCmdLine is the status bar while typing a command.
func (m *Model) renderCmdLine() string {
	m.cmd.input.Width = max(10, m.width-2)
	return ansi.Truncate(m.cmd.input.View(), m.width, "")
}

// overlayCmdCompletion draws the suggestions over the bottom of the body.
func (m *Model) overlayCmdCompletion(body string) string {
	if m.cmd == nil || m.cmd.complete == nil {
		return body
	}
	rows := append([]string{rule(m.width, true)}, m.renderCompItems(m.cmd.complete)...)
	lines := strings.Split(body, "\n")
	if len(rows) >= len(lines) {
		return body
	}
	start := len(lines) - len(rows)
	for i, r := range rows {
		r = ansi.Truncate(r, m.width, "…")
		if pad := m.width - ansi.StringWidth(r); pad > 0 {
			r += strings.Repeat(" ", pad)
		}
		lines[start+i] = r
	}
	return strings.Join(lines, "\n")
}
