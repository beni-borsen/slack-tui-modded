package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// searchState is the `/` message search. It takes over the main pane while
// open and is kept when closed, so `/` brings the last results back.
type searchState struct {
	input  textinput.Model
	open   bool
	typing bool // the query box has the keys; otherwise the results do

	query   string // what the results are for
	hits    []backend.Hit
	total   int
	page    int
	pages   int
	loading bool
	err     string
	seq     int // ignores answers to queries that were replaced

	sel, top int

	complete  *completion // people and channels while typing from:@an, in:#de
	dismissed string
}

type searchMsg struct {
	seq   int
	query string
	more  bool // a further page of the same query
	res   backend.SearchPage
	err   error
}

// jumpState is a search result being brought into view: the conversation
// (or thread) is loaded, and older pages fetched until the message is in it.
type jumpState struct {
	conv, ts string
	thread   string // parent ts when the message is a thread reply
	pages    int    // older pages fetched so far
}

const (
	jumpPageSize = 200
	jumpMaxPages = 10 // give up after ~2000 messages
)

// Matched words: bold amber, then back to the default colour.
const (
	hitOn  = "\x1b[1;38;5;221m"
	hitOff = "\x1b[22;39m"
)

func (m *Model) openSearch() tea.Cmd {
	if m.search == nil {
		ti := textinput.New()
		ti.Prompt = "/ "
		ti.Placeholder = "search messages — from:@anna in:#dev after:2026-01-01 \"exact words\""
		m.search = &searchState{input: ti}
	}
	s := m.search
	s.open, s.typing = true, true
	s.input.CursorEnd()
	return s.input.Focus()
}

func (m *Model) closeSearch() {
	if s := m.search; s != nil {
		s.open, s.typing = false, false
		s.input.Blur()
	}
}

func (m *Model) runSearch(query string, page int) tea.Cmd {
	s := m.search
	if page == 1 {
		s.seq++
		s.query, s.hits, s.total, s.page, s.pages, s.sel, s.top, s.err = query, nil, 0, 0, 0, 0, 0, ""
	}
	s.loading = true
	seq := s.seq
	return func() tea.Msg {
		res, err := m.client.Search(m.ctx, query, page)
		return searchMsg{seq: seq, query: query, more: page > 1, res: res, err: err}
	}
}

func (m *Model) searchResult(msg searchMsg) tea.Cmd {
	s := m.search
	if s == nil || msg.seq != s.seq {
		return nil
	}
	s.loading = false
	if msg.err != nil {
		s.err = msg.err.Error()
		return nil
	}
	if !msg.more {
		s.hits = nil
	}
	s.hits = append(s.hits, msg.res.Hits...)
	s.total, s.page, s.pages = msg.res.Total, msg.res.Page, msg.res.Pages
	return nil
}

func (m *Model) searchKey(k tea.KeyMsg) tea.Cmd {
	s := m.search
	if s.typing {
		if s.complete != nil {
			if cmd, ok := m.searchCompletionKey(k); ok {
				return cmd
			}
		}
		switch k.String() {
		case "enter":
			q := strings.TrimSpace(s.input.Value())
			if q == "" {
				return nil
			}
			s.typing, s.complete = false, nil
			s.input.Blur()
			return m.runSearch(q, 1)
		case "esc":
			if s.query == "" {
				m.closeSearch()
				return nil
			}
			s.input.SetValue(s.query) // back to the results it shows
			s.typing, s.complete = false, nil
			s.input.Blur()
			return nil
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(k)
		m.updateSearchCompletion()
		return cmd
	}

	n := len(s.hits)
	switch k.String() {
	case "esc":
		m.closeSearch()
		return nil
	case "q":
		return m.quit()
	case "/", "i", "a":
		s.typing = true
		s.input.CursorEnd()
		return s.input.Focus()
	case "ctrl+k":
		m.closeSearch()
		m.switcher = m.newSwitcher()
		return nil
	case "j", "down":
		s.sel++
	case "k", "up":
		s.sel--
	case "ctrl+d":
		s.sel += 5
	case "ctrl+u":
		s.sel -= 5
	case "g", "home":
		s.sel = 0
	case "G", "end":
		s.sel = n - 1
	case "enter", "l", "right":
		if n > 0 {
			return m.jumpTo(s.hits[min(max(s.sel, 0), n-1)])
		}
	case "o":
		if n > 0 {
			h := s.hits[min(max(s.sel, 0), n-1)]
			return tea.Batch(m.openURL(h.Permalink), m.setStatus("opening in Slack…", false))
		}
	}
	s.sel = min(max(s.sel, 0), max(n-1, 0))
	// Near the end: fetch the next page.
	if n > 0 && s.sel >= n-3 && s.page < s.pages && !s.loading {
		return m.runSearch(s.query, s.page+1)
	}
	return nil
}

// jumpTo opens the conversation (and thread) of a search result and
// selects the message once it has loaded.
func (m *Model) jumpTo(h backend.Hit) tea.Cmd {
	if m.convs[h.Channel] == nil {
		// Not a member (a public channel found by search): Slack can show it.
		return tea.Batch(m.openURL(h.Permalink), m.setStatus("not a member of "+m.hitWhere(h)+", opening in Slack…", false))
	}
	m.closeSearch()
	return m.goToMessage(h.Channel, h.Message)
}

// goToMessage opens msg's conversation (and thread, for a reply) and
// selects it once it has loaded.
func (m *Model) goToMessage(conv string, msg backend.Message) tea.Cmd {
	j := &jumpState{conv: conv, ts: msg.TS}
	if msg.ThreadTS != "" && msg.ThreadTS != msg.TS {
		j.thread = msg.ThreadTS
	}
	m.jump = j
	// open() fetches the newest page (and openThread the replies); the
	// target is looked for when that arrives, since merging the fresh page
	// can move messages around.
	cmd := m.open(conv)
	if j.thread == "" {
		return cmd
	}
	parent := backend.Message{TS: j.thread}
	if l := m.lists[conv]; l != nil {
		if i := msgIndex(l.msgs, j.thread); i >= 0 {
			parent = l.msgs[i]
		}
	}
	return tea.Batch(cmd, m.openThread(parent))
}

// resolveJump runs when the jump's conversation page or thread has been
// fetched: it selects the target, or fetches older history when the message
// may be further back.
func (m *Model) resolveJump() tea.Cmd {
	j := m.jump
	if j == nil {
		return nil
	}
	if m.current != j.conv {
		m.jump = nil // the user went elsewhere
		return nil
	}
	if j.thread != "" {
		t := m.thread
		if t == nil || t.conv != j.conv || t.ts != j.thread {
			m.jump = nil
			return nil
		}
		if i := msgIndex(t.list.msgs, j.ts); i >= 0 {
			t.list.sel = i
			if i == len(t.list.msgs)-1 {
				t.list.sel = -1
			}
			m.jump = nil
			return nil
		}
		m.jump = nil
		return m.setStatus("that reply is gone from the thread", true)
	}

	l := m.lists[j.conv]
	if l == nil {
		m.jump = nil
		return nil
	}
	if i := msgIndex(l.msgs, j.ts); i >= 0 {
		l.sel = i
		if i == len(l.msgs)-1 {
			l.sel = -1
		}
		m.jump = nil
		return nil
	}
	older := len(l.msgs) > 0 && backend.TSLess(j.ts, l.msgs[0].TS)
	if older && l.hasMore && j.pages < jumpMaxPages {
		j.pages++
		return m.loadHistory(j.conv, true)
	}
	m.jump = nil
	if older && l.hasMore {
		return m.setStatus("too far back to load here: o on the result opens it in Slack", true)
	}
	return m.setStatus("message not found, it may have been deleted", true)
}

// unmarkEntities drops match markers inside <…> (links, mentions), where
// they would end up in a URL or an ID.
func unmarkEntities(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case in && (string(r) == backend.HitStart || string(r) == backend.HitEnd):
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// A person or channel being typed in the query: "@an", "#de", or a bare
// name after from:/to:/with:/in:. Slack's search only knows handles
// (from:@anna.jensen), not display names, so picking one inserts that.
var reSearchName = regexp.MustCompile(`(?:^|\s)((?:from|to|with|in):)?([@#]?)([\p{L}\p{N}._\-]+)$`)

func (m *Model) updateSearchCompletion() {
	s := m.search
	before := []rune(s.input.Value())
	before = before[:min(s.input.Position(), len(before))]
	g := reSearchName.FindStringSubmatch(string(before))
	if g == nil || (g[1] == "" && g[2] == "") {
		s.complete, s.dismissed = nil, ""
		return
	}
	trigger, query := g[2], g[3]
	if trigger == "" {
		trigger = "@"
		if g[1] == "in:" {
			trigger = "#"
		}
	}
	if g[1]+g[2]+query == s.dismissed {
		s.complete = nil
		return
	}
	if c := s.complete; c != nil && c.trigger == trigger && c.query == g[2]+query {
		return
	}
	var items []compItem
	if trigger == "#" {
		items = m.matchChannels(query)
	} else {
		for _, it := range m.matchPeople(query) {
			id, ok := strings.CutPrefix(strings.TrimSuffix(it.token, ">"), "<@")
			if !ok {
				continue // @here and friends mean nothing to search
			}
			it.insert = "@" + m.users[id].Name
			it.label, it.detail = strings.TrimPrefix(it.label, "@"), it.insert
			if id == m.client.SelfID {
				it.detail += " · you"
			}
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		s.complete = nil
		return
	}
	// query keeps a typed trigger, so accepting replaces both.
	s.complete = &completion{trigger: trigger, query: g[2] + query, items: items}
	s.dismissed = ""
}

func (m *Model) searchCompletionKey(k tea.KeyMsg) (tea.Cmd, bool) {
	s := m.search
	c := s.complete
	switch k.String() {
	case "tab", "enter":
		it := c.items[c.sel]
		v := []rune(s.input.Value())
		pos := min(s.input.Position(), len(v))
		start := pos - len([]rune(c.query))
		out := string(v[:start]) + it.insert + " "
		s.input.SetValue(out + string(v[pos:]))
		s.input.SetCursor(len([]rune(out)))
		s.complete = nil
		return nil, true
	case "down", "ctrl+n":
		c.sel = min(c.sel+1, len(c.items)-1)
		return nil, true
	case "up", "ctrl+p":
		c.sel = max(c.sel-1, 0)
		return nil, true
	case "esc":
		before := []rune(s.input.Value())
		if g := reSearchName.FindStringSubmatch(string(before[:min(s.input.Position(), len(before))])); g != nil {
			s.dismissed = g[1] + g[2] + g[3]
		}
		s.complete = nil
		return nil, true
	}
	return nil, false
}

func msgIndex(msgs []backend.Message, ts string) int {
	for i := range msgs {
		if msgs[i].TS == ts {
			return i
		}
	}
	return -1
}

// hitWhere names the conversation a result is in.
func (m *Model) hitWhere(h backend.Hit) string {
	if c := m.convs[h.Channel]; c != nil {
		if c.IsDM() {
			return m.convLabel(c)
		}
		return "#" + c.Name
	}
	switch {
	case h.IsGroup:
		return "group message"
	case strings.HasPrefix(h.ChannelName, "U") || strings.HasPrefix(h.ChannelName, "W"):
		if n := m.UserName(h.ChannelName); n != "" {
			return n
		}
		return "direct message"
	}
	return "#" + h.ChannelName
}

func (m *Model) renderSearch(w, h int) string {
	s := m.search
	title := sTitle.Render("Search")
	switch {
	case s.query == "":
	case s.loading && len(s.hits) == 0:
		title += sDim.Render("  searching…")
	case s.err == "":
		title += sDim.Render(fmt.Sprintf("  %d %s", s.total, plural(s.total, "result", "results")))
	}
	s.input.Width = max(10, w-4)
	inputLine := "  " + s.input.View()
	if !s.typing {
		inputLine = "  " + sDim.Render("/ "+s.input.Value())
	}
	header := []string{title, rule(w, !s.typing), inputLine}
	if s.typing && s.complete != nil {
		header = append(header, m.renderCompItems(s.complete)...)
	}
	header = append(header, rule(w, s.typing))
	var footer string
	if s.typing {
		footer = sFaint.Render("  ") + sKey.Render("enter") + sFaint.Render(" search  ") + sKey.Render("esc") + sFaint.Render(" back")
	} else {
		footer = sFaint.Render("  ") + sKey.Render("enter") + sFaint.Render(" go to message  ") +
			sKey.Render("o") + sFaint.Render(" open in Slack  ") + sKey.Render("/") + sFaint.Render(" edit query  ") +
			sKey.Render("esc") + sFaint.Render(" close")
	}
	bodyH := max(1, h-len(header)-2)

	switch {
	case s.err != "":
		return strings.Join(append(header, placeholder(sErr.Render(s.err), w, bodyH), rule(w, false), footer), "\n")
	case s.query == "":
		return strings.Join(append(header, placeholder("type a query and press enter", w, bodyH), rule(w, false), footer), "\n")
	case len(s.hits) == 0:
		msg := "no results"
		if s.loading {
			msg = "searching…"
		}
		return strings.Join(append(header, placeholder(msg, w, bodyH), rule(w, false), footer), "\n")
	}

	sel := min(max(s.sel, 0), len(s.hits)-1)
	var all []string
	var selStart, selEnd int
	for i, hit := range s.hits {
		bar := "  "
		if i == sel {
			bar = sBar.Render("▌") + " "
			if s.typing {
				bar = sBarDim.Render("▌") + " "
			}
			selStart = len(all)
		}
		icon := "" // channels already read "#name"
		switch c := m.convs[hit.Channel]; {
		case c != nil && c.Kind == backend.KindPrivate:
			icon = lockIcon + " "
		case c != nil && c.Kind == backend.KindGroup, c == nil && hit.IsGroup:
			icon = groupIcon + " "
		case c != nil && c.Kind == backend.KindDM:
			icon = userIcon + " "
		}
		where := sUnread.Render(icon + m.hitWhere(hit))
		if hit.ThreadTS != "" && hit.ThreadTS != hit.TS {
			where += sThread.Render("  in thread")
		}
		name := nameStyle(senderKey(hit.Message)).Render(m.senderName(hit.Message))
		block := []string{where, name + " " + sTime.Render(relTime(hit.Time(), m.cfg.TimeFormat))}
		text := strings.ReplaceAll(render.Mrkdwn(unmarkEntities(hit.Text), m, mdStyles), "\n", " ")
		text = strings.NewReplacer(backend.HitStart, hitOn, backend.HitEnd, hitOff).Replace(text)
		wrapped := wrap(text, w-4)
		if len(wrapped) > 3 {
			wrapped = append(wrapped[:3:3], sFaint.Render("…"))
		}
		block = append(block, wrapped...)
		for _, l := range block {
			all = append(all, bar+l)
		}
		if i == sel {
			selEnd = len(all)
		}
		all = append(all, "")
	}
	if s.page < s.pages {
		all = append(all, sFaint.Render(center("more below…", w)))
	}

	if selStart < s.top {
		s.top = selStart
	}
	if selEnd > s.top+bodyH {
		s.top = max(selStart, selEnd-bodyH)
	}
	s.top = min(max(s.top, 0), max(0, len(all)-bodyH))
	view := all[s.top:min(len(all), s.top+bodyH)]
	for len(view) < bodyH {
		view = append(view, "")
	}
	for i := range view {
		view[i] = ansi.Truncate(view[i], w, "…")
	}
	return strings.Join(append(append(header, view...), rule(w, false), footer), "\n")
}
