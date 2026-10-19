package tui

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// pinsState is `P`: a conversation's bookmarks, then its pinned messages.
// It takes over the main pane like search.
type pinsState struct {
	conv      string
	bookmarks []backend.Bookmark
	pins      []backend.Pin
	loading   bool
	errs      []string // bookmarks or pins that failed; the other still shows
	sel, top  int
}

type pinsMsg struct {
	conv      string
	bookmarks []backend.Bookmark
	pins      []backend.Pin
	errs      []string
}

type pinnedMsg struct {
	conv, ts string
	added    bool
	err      error
}

func (p *pinsState) count() int { return len(p.bookmarks) + len(p.pins) }

func (m *Model) openPins() tea.Cmd {
	if m.current == unreadsID || m.convs[m.current] == nil {
		return m.setStatus("open a conversation first", false)
	}
	m.closeSearch()
	m.pins = &pinsState{conv: m.current, loading: true}
	return m.loadPins(m.current)
}

func (m *Model) loadPins(conv string) tea.Cmd {
	return func() tea.Msg {
		out := pinsMsg{conv: conv}
		var bErr, pErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); out.bookmarks, bErr = m.client.Bookmarks(m.ctx, conv) }()
		go func() { defer wg.Done(); out.pins, pErr = m.client.Pins(m.ctx, conv) }()
		wg.Wait()
		for _, err := range []error{bErr, pErr} {
			if err != nil {
				out.errs = append(out.errs, err.Error())
			}
		}
		return out
	}
}

func (m *Model) pinsResult(msg pinsMsg) {
	p := m.pins
	if p == nil || p.conv != msg.conv {
		return
	}
	p.loading = false
	p.bookmarks, p.pins, p.errs = msg.bookmarks, msg.pins, msg.errs
	p.sel = min(p.sel, max(p.count()-1, 0))
}

func (m *Model) pinsKey(k tea.KeyMsg) tea.Cmd {
	p := m.pins
	n := p.count()
	switch k.String() {
	case "esc", "P":
		m.pins = nil
		return nil
	case "q":
		return m.quit()
	case "/":
		m.pins = nil
		return m.openSearch()
	case "ctrl+k":
		m.pins = nil
		m.switcher = m.newSwitcher()
		return nil
	case "j", "down":
		p.sel++
	case "k", "up":
		p.sel--
	case "g", "home":
		p.sel = 0
	case "G", "end":
		p.sel = n - 1
	case "enter", "l", "right":
		if n == 0 {
			return nil
		}
		if b, pin := p.at(p.sel); b != nil {
			return m.openURL(b.Link)
		} else if pin.TS == "" { // a file pinned on its own
			return m.openURL(pin.Permalink)
		} else {
			m.pins = nil
			return m.goToMessage(pin.Channel, pin.Message)
		}
	case "o":
		if n == 0 {
			return nil
		}
		if b, pin := p.at(p.sel); b != nil {
			return m.openURL(b.Link)
		} else {
			return tea.Batch(m.openURL(pin.Permalink), m.setStatus("opening in Slack…", false))
		}
	case "p":
		if _, pin := p.at(p.sel); pin != nil && pin.TS != "" {
			return m.togglePin(pin.Channel, pin.Message)
		}
	}
	p.sel = min(max(p.sel, 0), max(n-1, 0))
	return nil
}

// at returns the bookmark or the pin at row i; both nil when the list is
// empty (or still loading).
func (p *pinsState) at(i int) (*backend.Bookmark, *backend.Pin) {
	if p.count() == 0 {
		return nil, nil
	}
	i = min(max(i, 0), p.count()-1)
	if i < len(p.bookmarks) {
		return &p.bookmarks[i], nil
	}
	return nil, &p.pins[i-len(p.bookmarks)]
}

// togglePin pins or unpins a message. Like reactions, the change shows at
// once and a failed call puts it back.
func (m *Model) togglePin(conv string, msg backend.Message) tea.Cmd {
	add := !msg.Pinned
	m.setPinned(conv, msg.TS, add)
	return func() tea.Msg {
		return pinnedMsg{conv: conv, ts: msg.TS, added: add, err: m.client.Pin(m.ctx, conv, msg.TS, add)}
	}
}

// setPinned marks a message pinned or not wherever it is loaded, and keeps
// an open pins list in step.
func (m *Model) setPinned(conv, ts string, pinned bool) {
	if ts == "" {
		return
	}
	m.touch(conv)
	mark := func(msgs []backend.Message) {
		if i := msgIndex(msgs, ts); i >= 0 {
			msgs[i].Pinned = pinned
		}
	}
	if l, ok := m.lists[conv]; ok {
		mark(l.msgs)
	}
	if t := m.thread; t != nil && t.conv == conv {
		mark(t.list.msgs)
	}
	if u, ok := m.unreadMsgs[conv]; ok {
		mark(u)
	}
	if p := m.pins; p != nil && p.conv == conv && !pinned {
		for i := range p.pins {
			if p.pins[i].TS == ts {
				p.pins = append(p.pins[:i], p.pins[i+1:]...)
				p.sel = min(p.sel, max(p.count()-1, 0))
				break
			}
		}
	}
}

// pinEvent applies a pin made anywhere; an open list of that conversation
// is fetched again, since the event doesn't carry the whole message.
func (m *Model) pinEvent(ev backend.EvPin) tea.Cmd {
	m.setPinned(ev.Channel, ev.TS, ev.Added)
	if p := m.pins; p != nil && p.conv == ev.Channel && (ev.Added || ev.TS == "") {
		return m.loadPins(ev.Channel)
	}
	return nil
}

func (m *Model) renderPins(w, h int) string {
	p := m.pins
	title := sTitle.Render("Pinned & bookmarks")
	if c := m.convs[p.conv]; c != nil {
		title += sDim.Render("  " + m.convTitle(c))
	}
	header := []string{title, rule(w, true)}
	footer := sFaint.Render("  ") + sKey.Render("enter") + sFaint.Render(" go to / open  ") +
		sKey.Render("o") + sFaint.Render(" open in Slack  ") + sKey.Render("p") + sFaint.Render(" unpin  ") +
		sKey.Render("esc") + sFaint.Render(" close")
	for _, e := range p.errs {
		header = append(header, "  "+sErr.Render(e))
	}
	bodyH := max(1, h-len(header)-2)
	if p.count() == 0 {
		msg := "nothing pinned or bookmarked"
		if p.loading {
			msg = "loading…"
		}
		return strings.Join(append(header, placeholder(msg, w, bodyH), rule(w, false), footer), "\n")
	}

	sel := min(max(p.sel, 0), p.count()-1)
	var all []string
	var selStart, selEnd int
	add := func(i int, block []string) {
		bar := "  "
		if i == sel {
			bar = sBar.Render("▌") + " "
			selStart = len(all)
		}
		for _, l := range block {
			all = append(all, bar+l)
		}
		if i == sel {
			selEnd = len(all)
		}
		all = append(all, "")
	}
	if len(p.bookmarks) > 0 {
		all = append(all, "  "+sHeader.Render("BOOKMARKS"), "")
	}
	for i, b := range p.bookmarks {
		label := b.Title
		if label == "" {
			label = b.Link
		}
		if name := strings.Trim(b.Emoji, ":"); name != "" {
			label = m.emojiChip(name) + " " + label
		}
		add(i, []string{sNormal.Render(label), sDim.Render(b.Link)})
	}
	if len(p.pins) > 0 {
		all = append(all, "  "+sHeader.Render("PINNED"), "")
	}
	for i, pin := range p.pins {
		var block []string
		if pin.TS == "" {
			for _, f := range pin.Files {
				name := f.Title
				if name == "" {
					name = f.Name
				}
				block = append(block, sNormal.Render(name), sDim.Render("file"))
			}
		} else {
			name := nameStyle(pin.User + pin.BotName + pin.BotID).Render(m.senderName(pin.Message))
			line := name + " " + sTime.Render(relTime(pin.Time(), m.cfg.TimeFormat))
			if pin.IsReply() {
				line += sThread.Render("  in thread")
			}
			block = append(block, line)
			text := strings.ReplaceAll(render.Mrkdwn(pin.Text, m, mdStyles), "\n", " ")
			if strings.TrimSpace(pin.Text) == "" && len(pin.Files) > 0 {
				text = sDim.Render(pin.Files[0].Name)
			}
			wrapped := wrap(text, w-4)
			if len(wrapped) > 3 {
				wrapped = append(wrapped[:3:3], sFaint.Render("…"))
			}
			block = append(block, wrapped...)
		}
		add(len(p.bookmarks)+i, block)
	}

	if selStart < p.top {
		p.top = selStart
	}
	if selEnd > p.top+bodyH {
		p.top = max(selStart, selEnd-bodyH)
	}
	if sel == 0 {
		p.top = 0 // keep the first section heading in view
	}
	p.top = min(max(p.top, 0), max(0, len(all)-bodyH))
	view := all[p.top:min(len(all), p.top+bodyH)]
	for len(view) < bodyH {
		view = append(view, "")
	}
	for i := range view {
		view[i] = ansi.Truncate(view[i], w, "…")
	}
	return strings.Join(append(append(header, view...), rule(w, false), footer), "\n")
}
