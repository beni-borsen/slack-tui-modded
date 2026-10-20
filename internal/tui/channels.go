package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

// Channels: `:join` browses the workspace's public channels (or joins one
// by name), `:leave`, `:create [--private] name`, `:invite @person`.

// chansState is the `:join` pane: every public channel, filtered as you type.
type chansState struct {
	input    textinput.Model
	matches  []int // indexes into m.public, best first
	sel, top int
	autoJoin string // `:join name`: joined at once if the list has exactly that channel
}

type publicChansMsg struct {
	chans []backend.PublicChannel
	err   error
}

type joinedMsg struct {
	conv backend.Conversation
	name string
	then *backend.Message // a search result to go to once joined
	err  error
}

type leftMsg struct {
	id, name string
	err      error
}

type createdMsg struct {
	conv backend.Conversation
	name string
	err  error
}

type invitedMsg struct {
	conv  string
	names []string
	err   error
}

func (m *Model) loadPublic() tea.Cmd {
	if m.publicLoading {
		return nil
	}
	m.publicLoading = true
	return func() tea.Msg {
		chans, err := m.client.PublicChannels(m.ctx)
		return publicChansMsg{chans, err}
	}
}

func (m *Model) cmdJoin(arg string) tea.Cmd {
	name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(arg)), "#")
	if name != "" {
		if c := m.convByName(name); c != nil {
			return m.open(c.ID)
		}
		if ch := m.publicByName(name); ch != nil {
			return m.join(ch.ID, ch.Name, nil)
		}
	}
	ti := textinput.New()
	ti.Prompt = "› "
	ti.Placeholder = "filter channels"
	ti.CharLimit = 80
	ti.SetValue(name)
	m.closeSearch()
	m.pins, m.sched, m.shared, m.members = nil, nil, nil, nil
	m.insert = false
	m.input.Blur()
	m.chans = &chansState{input: ti, autoJoin: name}
	m.filterChans()
	// Fetched every time the pane opens, so new channels show up.
	return tea.Batch(m.chans.input.Focus(), m.loadPublic())
}

// convByName finds a channel we are in.
func (m *Model) convByName(name string) *backend.Conversation {
	for _, c := range m.convs {
		if !c.IsDM() && strings.EqualFold(c.Name, name) {
			return c
		}
	}
	return nil
}

func (m *Model) publicByName(name string) *backend.PublicChannel {
	for i := range m.public {
		if strings.EqualFold(m.public[i].Name, name) {
			return &m.public[i]
		}
	}
	return nil
}

func (m *Model) publicResult(msg publicChansMsg) tea.Cmd {
	m.publicLoading = false
	if msg.err != nil {
		if m.chans != nil {
			return m.setStatus(msg.err.Error(), true)
		}
		return nil
	}
	m.public = msg.chans
	m.syncPublic()
	var cmd tea.Cmd
	if m.cmd != nil {
		cmd = m.updateCmdCompletion() // ":join " was waiting for the list
	}
	s := m.chans
	if s == nil {
		return cmd
	}
	m.filterChans()
	if name := s.autoJoin; name != "" {
		s.autoJoin = ""
		if ch := m.publicByName(name); ch != nil && !ch.IsMember {
			m.chans = nil
			return m.join(ch.ID, ch.Name, nil)
		}
	}
	return nil
}

// syncPublic marks the channels we are in, since membership changes after
// the list was fetched.
func (m *Model) syncPublic() {
	for i := range m.public {
		m.public[i].IsMember = m.convs[m.public[i].ID] != nil
	}
}

// argCompletion suggests the argument of :join (public channels we aren't
// in), :leave (channels we are in) and :invite (people, by handle). ok says
// the text is one of those commands, which may still have no suggestions.
func (m *Model) argCompletion(text string) (cp *completion, ok bool, load tea.Cmd) {
	name, rest, _ := strings.Cut(text, " ")
	c, found := findCommand(name)
	if !found {
		return nil, false, nil
	}
	word := rest[strings.LastIndex(rest, " ")+1:]
	var items []compItem
	switch c.name {
	case "join":
		if strings.Contains(rest, " ") {
			return nil, true, nil
		}
		if m.public == nil {
			return nil, true, m.loadPublic()
		}
		for _, i := range rank(strings.TrimPrefix(word, "#"), len(m.public),
			func(i int) []string { return []string{m.public[i].Name} },
			func(i int) int { return len(m.public[i].Name) }) {
			ch := m.public[i]
			if ch.IsMember {
				continue
			}
			items = append(items, compItem{insert: "#" + ch.Name, label: "#" + ch.Name, detail: fmt.Sprintf("%d members", ch.Members)})
			if len(items) == completeRows*4 {
				break
			}
		}
	case "leave":
		if strings.Contains(rest, " ") {
			return nil, true, nil
		}
		items = m.matchChannels(strings.TrimPrefix(word, "#"))
	case "invite", "dm", "profile":
		for _, it := range m.matchPeople(strings.TrimPrefix(word, "@")) {
			id := strings.TrimSuffix(strings.TrimPrefix(it.token, "<@"), ">")
			u, isUser := m.users[id]
			if !isUser || u.ID == m.client.SelfID || u.IsBot {
				continue // the broadcasts, and you
			}
			it.insert = "@" + u.Name
			items = append(items, it)
		}
	default:
		return nil, false, nil
	}
	if len(items) == 0 {
		return nil, true, nil
	}
	return &completion{trigger: "arg", query: word, items: items}, true, nil
}

func (m *Model) filterChans() {
	s := m.chans
	q := strings.TrimPrefix(strings.TrimSpace(s.input.Value()), "#")
	s.sel, s.top = 0, 0
	if q == "" {
		s.matches = s.matches[:0]
		for i := range m.public {
			s.matches = append(s.matches, i) // by name
		}
		return
	}
	s.matches = rank(q, len(m.public),
		func(i int) []string { return []string{m.public[i].Name} },
		func(i int) int { return len(m.public[i].Name) })
	s.sel, s.top = 0, 0
}

func (m *Model) chansKey(k tea.KeyMsg) tea.Cmd {
	s := m.chans
	n := len(s.matches)
	page := max(1, m.height/2)
	switch k.String() {
	case "esc":
		m.chans = nil
		return nil
	case "enter":
		if n == 0 {
			return nil
		}
		ch := m.public[s.matches[min(s.sel, n-1)]]
		m.chans = nil
		if m.convs[ch.ID] != nil {
			return m.open(ch.ID)
		}
		return m.join(ch.ID, ch.Name, nil)
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
			m.filterChans()
		}
		return cmd
	}
	s.sel = min(max(s.sel, 0), max(n-1, 0))
	return nil
}

// join joins a channel, then opens it (or goes to then, a search result).
func (m *Model) join(id, name string, then *backend.Message) tea.Cmd {
	return tea.Batch(m.setStatus("joining #"+name+"…", false), func() tea.Msg {
		c, err := m.client.Join(m.ctx, id)
		return joinedMsg{conv: c, name: name, then: then, err: err}
	})
}

func (m *Model) joinedResult(msg joinedMsg) tea.Cmd {
	if msg.err != nil {
		return m.setStatus("couldn't join #"+msg.name+": "+slackReason(msg.err), true)
	}
	m.addConv(msg.conv)
	status := m.setStatus("joined #"+msg.conv.Name, false)
	if msg.then != nil {
		return tea.Batch(status, m.goToMessage(msg.conv.ID, *msg.then))
	}
	return tea.Batch(status, m.open(msg.conv.ID))
}

// addConv shows a conversation we just joined or made, without waiting for
// the conversation list to be fetched again.
func (m *Model) addConv(c backend.Conversation) {
	m.mergeConv(c)
	m.syncPublic()
	m.rebuildSidebar()
}

// cmdLeave leaves this conversation, or the channel named.
func (m *Model) cmdLeave(arg string) tea.Cmd {
	var c *backend.Conversation
	if name := strings.TrimPrefix(strings.TrimSpace(arg), "#"); name != "" {
		if c = m.convByName(name); c == nil {
			return m.setStatus("you're not in #"+name, true)
		}
	} else if c = m.convs[m.current]; c == nil {
		return m.setStatus("open a channel first, or :leave #name", false)
	}
	if c.Kind == backend.KindDM {
		return m.setStatus("direct messages can't be left", false)
	}
	where := m.convRef(c)
	prompt := "leave " + where + "? y/n"
	if c.Kind != backend.KindChannel {
		prompt = "leave " + where + "? only an invite brings you back. y/n"
	}
	id := c.ID
	m.confirm = &confirmState{
		prompt: prompt,
		yes: func() tea.Cmd {
			return func() tea.Msg { return leftMsg{id: id, name: where, err: m.client.Leave(m.ctx, id)} }
		},
	}
	return nil
}

// convRef names a conversation in a sentence: "#dev", or the people of a
// group DM.
func (m *Model) convRef(c *backend.Conversation) string {
	if c.IsDM() {
		return m.convLabel(c)
	}
	return "#" + c.Name
}

func (m *Model) leftResult(msg leftMsg) tea.Cmd {
	if msg.err != nil {
		return m.setStatus("couldn't leave "+msg.name+": "+slackReason(msg.err), true)
	}
	return tea.Batch(m.dropConv(msg.id), m.setStatus("left "+msg.name, false))
}

// dropConv forgets a conversation we are no longer in, and leaves it if it
// is on screen.
func (m *Model) dropConv(id string) tea.Cmd {
	delete(m.convs, id)
	delete(m.lists, id)
	delete(m.unreadMsgs, id)
	if t := m.thread; t != nil && t.conv == id {
		m.thread = nil
	}
	if p := m.pins; p != nil && p.conv == id {
		m.pins = nil
	}
	m.syncPublic()
	if m.current == id {
		return m.open(unreadsID)
	}
	m.rebuildSidebar()
	return nil
}

// cmdCreate makes a channel. Like Slack, the name is lower-cased and spaces
// become dashes.
func (m *Model) cmdCreate(args string) tea.Cmd {
	private := false
	var words []string
	for _, w := range strings.Fields(args) {
		switch w {
		case "--private", "-p":
			private = true
		case "--public":
			private = false
		default:
			words = append(words, w)
		}
	}
	name := strings.TrimPrefix(strings.ToLower(strings.Join(words, "-")), "#")
	if name == "" {
		return m.setStatus("usage: :create [--private] name", false)
	}
	if c := m.convByName(name); c != nil {
		return m.setStatus("#"+name+" already exists, and you're in it", false)
	}
	kind := "public"
	if private {
		kind = "private"
	}
	m.confirm = &confirmState{
		prompt: "create " + kind + " channel #" + name + "? y/n",
		yes: func() tea.Cmd {
			return func() tea.Msg {
				c, err := m.client.Create(m.ctx, name, private)
				return createdMsg{conv: c, name: name, err: err}
			}
		},
	}
	return nil
}

func (m *Model) createdResult(msg createdMsg) tea.Cmd {
	if msg.err != nil {
		reason := slackReason(msg.err)
		if strings.HasSuffix(msg.err.Error(), "name_taken") {
			reason = "the name is taken (:join " + msg.name + " if it's public)"
		}
		return m.setStatus("couldn't create #"+msg.name+": "+reason, true)
	}
	m.addConv(msg.conv)
	return tea.Batch(m.open(msg.conv.ID), m.setStatus("created #"+msg.conv.Name+" — :invite @person to add people", false))
}

// cmdInvite adds people to the open channel: ":invite @anna @bob".
func (m *Model) cmdInvite(args string) tea.Cmd {
	c := m.convs[m.current]
	if c == nil || c.IsDM() {
		return m.setStatus("open a channel first", false)
	}
	var ids, names []string
	for _, w := range strings.Fields(args) {
		u, err := m.userByHandle(w)
		if err != nil {
			return m.setStatus(err.Error(), true)
		}
		ids = append(ids, u.ID)
		names = append(names, "@"+u.Name)
	}
	if len(ids) == 0 {
		return m.setStatus("usage: :invite @person [@person…]", false)
	}
	id := c.ID
	return func() tea.Msg {
		return invitedMsg{conv: id, names: names, err: m.client.Invite(m.ctx, id, ids)}
	}
}

// userByHandle finds a person by handle (what completion inserts), or by a
// display or real name that only they have.
func (m *Model) userByHandle(w string) (backend.User, error) {
	h := strings.ToLower(strings.TrimPrefix(w, "@"))
	var byName []backend.User
	for _, u := range m.users {
		if u.Deleted || u.IsBot {
			continue
		}
		if strings.ToLower(u.Name) == h {
			if u.ID == m.client.SelfID {
				return u, fmt.Errorf("@%s is you", u.Name)
			}
			return u, nil
		}
		if strings.EqualFold(u.DisplayName, h) || strings.EqualFold(u.RealName, h) {
			byName = append(byName, u)
		}
	}
	switch len(byName) {
	case 1:
		if byName[0].ID == m.client.SelfID {
			return byName[0], fmt.Errorf("@%s is you", byName[0].Name)
		}
		return byName[0], nil
	case 0:
		return backend.User{}, fmt.Errorf("nobody called @%s", h)
	}
	return backend.User{}, fmt.Errorf("several people are called %s; use their @handle", h)
}

func (m *Model) invitedResult(msg invitedMsg) tea.Cmd {
	where := "the channel"
	if c := m.convs[msg.conv]; c != nil {
		where = m.convRef(c)
	}
	if msg.err != nil {
		return m.setStatus("couldn't invite to "+where+": "+slackReason(msg.err), true)
	}
	return m.setStatus("invited "+strings.Join(msg.names, ", ")+" to "+where, false)
}

// slackReason turns the error codes these calls answer with into words.
func slackReason(err error) string {
	s := err.Error()
	code := s[strings.LastIndex(s, ": ")+1:]
	code = strings.TrimSpace(code)
	switch {
	case code == "name_taken":
		return "the name is taken"
	case code == "invalid_name_maxlength":
		return "names are at most 80 characters"
	case strings.HasPrefix(code, "invalid_name"):
		return "names can only have lowercase letters, numbers, - and _"
	case code == "already_in_channel":
		return "already in it"
	case code == "cant_invite_self":
		return "that's you"
	case code == "cant_leave_general":
		return "nobody can leave the workspace's general channel"
	case code == "is_archived":
		return "it's archived"
	case code == "restricted_action":
		return "your workspace doesn't let you"
	case code == "user_is_restricted":
		return "guests can't do that"
	case code == "method_not_supported_for_channel_type":
		return "not possible in this kind of conversation"
	case code == "missing_scope":
		return "the app is missing a scope; update it from manifest.yaml"
	}
	return s
}

func (m *Model) renderChans(w, h int) string {
	s := m.chans
	s.input.Width = max(10, w-6)
	title := sTitle.Render("Channels")
	if m.public != nil {
		title += sDim.Render(fmt.Sprintf("  %d public", len(m.public)))
	}
	header := []string{title, rule(w, true), " " + s.input.View(), ""}
	footer := sFaint.Render("  ") + sKey.Render("enter") + sFaint.Render(" join / open  ") +
		sKey.Render("↑ ↓") + sFaint.Render(" move  ") + sKey.Render("esc") + sFaint.Render(" close")
	bodyH := max(1, h-len(header)-2)
	if len(s.matches) == 0 {
		msg := "no channel matches"
		if m.public == nil {
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
		ch := m.public[s.matches[i]]
		name := "#" + ch.Name
		members := "1 member"
		if ch.Members != 1 {
			members = fmt.Sprintf("%d members", ch.Members)
		}
		info := members
		if ch.IsMember {
			info = "joined · " + info
		}
		if p := strings.Join(strings.Fields(ch.Purpose), " "); p != "" {
			info += " · " + p
		}
		var row string
		if i == sel {
			row = sBar.Render("▌") + " " + sActive.Render(name)
		} else if ch.IsMember {
			row = "  " + sNormal.Render(name)
		} else {
			row = "  " + sUnread.Render(name)
		}
		rows = append(rows, ansi.Truncate(row+"  "+sDim.Render(info), w, "…"))
	}
	for len(rows) < bodyH {
		rows = append(rows, "")
	}
	return strings.Join(append(append(header, rows...), rule(w, false), footer), "\n")
}
