package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// `?`: every key and command, filtered as you type. Typing a key ("P")
// finds what it does; words find keys ("pin", "thread").

var helpKeys = [][2]string{
	{"ctrl+k / space", "jump to a conversation, or a person to start one with"},
	{"j k  ↑ ↓", "move; a message taller than the pane scrolls first"},
	{"g G", "top / bottom"},
	{"ctrl+d ctrl+u", "half page"},
	{"h l  tab", "move between panes"},
	{"enter", "open conversation / thread"},
	{"i a", "write a message (in the focused pane)"},
	{"enter  (insert)", "send"},
	{"alt+enter ctrl+j", "new line"},
	{"ctrl+v  (insert)", "attach clipboard image (again for more)"},
	{"ctrl+b alt+i  (insert)", "bold / italic the word at the cursor"},
	{"ctrl+o  (insert, thread)", "also send the reply to the channel"},
	{"ctrl+s  (insert)", "send later: :later 30m / 15:30 / tomorrow / fri 9"},
	{":ta @an #de  (insert)", "complete emoji, people, channels"},
	{"esc", "back to normal mode"},
	{"/", "search messages (from:@x in:#y after:date); enter goes there"},
	{"n N", "next / previous search result, without the list"},
	{":", "command line (below); tab completes, ↑ ↓ history"},
	{"u", "next unread conversation"},
	{"r", "mark conversation read"},
	{"U", "mark unread from the message"},
	{"z  enter on header", "fold / unfold sidebar section"},
	{"}", "move the sidebar to the other side"},
	{"]", "hide / show the sidebar"},
	{"+", "react to the message"},
	{"c", "copy the message text"},
	{"e", "edit your message"},
	{"↑  (empty input)", "edit your last message"},
	{"D", "delete your message"},
	{"p", "pin / unpin the message"},
	{"P", "pinned messages and bookmarks; a e d add / edit / remove a bookmark"},
	{"F", "files and links shared in the conversation"},
	{"v", "view images full-window: ← → browse, o open, esc close"},
	{"o", "open the message's file or link (a chooser when there are several)"},
	{"s", "save the message's files to Downloads"},
	{"I", "profile of the message's sender (or the DM's person)"},
	{"x  (thread)", "close thread"},
	{"q", "quit"},
}

type helpState struct {
	input    textinput.Model
	entries  []helpEntry
	matches  []int // into entries
	sel, top int
}

type helpEntry struct {
	key, desc, args string
	command         bool
}

func (m *Model) openHelp() tea.Cmd {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.Placeholder = "filter keys and commands"
	ti.CharLimit = 60
	h := &helpState{input: ti}
	for _, r := range helpKeys {
		h.entries = append(h.entries, helpEntry{key: r[0], desc: r[1]})
	}
	for _, c := range commands {
		h.entries = append(h.entries, helpEntry{key: ":" + c.name, desc: c.help, args: c.args, command: true})
	}
	m.help = h
	h.filter()
	return h.input.Focus()
}

// filter ranks entries: every word of the query must match the key or the
// description, fuzzily; the key itself, then whole words, rank first.
func (h *helpState) filter() {
	words := strings.Fields(h.input.Value())
	h.matches = h.matches[:0]
	h.sel, h.top = 0, 0
	if len(words) == 0 {
		for i := range h.entries {
			h.matches = append(h.matches, i)
		}
		return
	}
	type hit struct{ i, score int }
	var hits []hit
	for i, e := range h.entries {
		total := 0
		for _, w := range words {
			s, ok := helpScore(w, e)
			if !ok {
				total = -1
				break
			}
			total += s
		}
		if total >= 0 {
			hits = append(hits, hit{i, total})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return b.score - a.score })
	for _, x := range hits {
		h.matches = append(h.matches, x.i)
	}
}

func helpScore(w string, e helpEntry) (int, bool) {
	best, ok := 0, false
	// The key, exactly as typed: "D" is delete, "d" isn't.
	for _, k := range strings.Fields(strings.NewReplacer("/", " ", "(", " ", ")", " ").Replace(e.key)) {
		if k == w || k == ":"+w {
			return 100, true
		}
	}
	lw := strings.ToLower(w)
	for _, field := range []string{e.key, e.desc, e.args} {
		s, matched := fuzzyScore([]rune(lw), []rune(field))
		if !matched {
			continue
		}
		lf, n := strings.ToLower(field), len([]rune(lw))
		switch {
		case strings.HasPrefix(lf, lw) || strings.Contains(lf, " "+lw) || strings.Contains(lf, ":"+lw):
			s = 50 + 10*n // a word, or the start of one
		case strings.Contains(lf, lw):
			s = 30 + 8*n // inside a word
		case s < 5*n:
			continue // letters scattered over the line: noise
		}
		if !ok || s > best {
			best, ok = s, true
		}
	}
	return best, ok
}

func (m *Model) helpKey(k tea.KeyMsg) tea.Cmd {
	h := m.help
	n := len(h.matches)
	switch k.String() {
	case "esc":
		m.help = nil
		return nil
	case "?":
		if h.input.Value() == "" {
			m.help = nil
			return nil
		}
	case "down", "ctrl+n", "ctrl+j", "tab":
		h.sel = min(h.sel+1, max(n-1, 0))
		return nil
	case "up", "ctrl+p", "shift+tab":
		h.sel = max(h.sel-1, 0)
		return nil
	case "pgdown", "ctrl+d":
		h.sel = min(h.sel+10, max(n-1, 0))
		return nil
	case "pgup", "ctrl+u":
		h.sel = max(h.sel-10, 0)
		return nil
	case "enter":
		m.help = nil
		if n > 0 {
			if e := h.entries[h.matches[h.sel]]; e.command {
				// Start the command, ready for its arguments.
				cmd := m.openCmd()
				m.cmd.input.SetValue(strings.TrimPrefix(e.key, ":") + " ")
				m.cmd.input.CursorEnd()
				return cmd
			}
		}
		return nil
	}
	before := h.input.Value()
	var cmd tea.Cmd
	h.input, cmd = h.input.Update(k)
	if h.input.Value() != before {
		h.filter()
	}
	return cmd
}

func (m *Model) renderHelp(width, height int) string {
	h := m.help
	boxW := min(width-2, 100)
	inner := boxW - 4
	h.input.Width = inner - 3
	keyW := 22
	rows := []string{h.input.View(), sFaint.Render(strings.Repeat("─", inner))}
	listH := max(3, height-6)

	var lines []string
	var selAt int
	filtering := strings.TrimSpace(h.input.Value()) != ""
	for k, i := range h.matches {
		e := h.entries[i]
		if !filtering && e.command && (k == 0 || !h.entries[h.matches[k-1]].command) {
			lines = append(lines, "", sTitle.Render("Commands"))
		}
		bar := "  "
		if k == h.sel {
			bar = sBar.Render("▌") + " "
			selAt = len(lines)
		}
		lines = append(lines, bar+sKey.Render(fmt.Sprintf("%-*s", keyW, e.key))+"  "+sNormal.Render(e.desc))
		if e.args != "" {
			lines = append(lines, "  "+strings.Repeat(" ", keyW+2)+sDim.Render(e.args))
		}
	}
	if len(h.matches) == 0 {
		lines = append(lines, sDim.Render("  nothing matches"))
	}
	if selAt < h.top {
		h.top = selAt
	}
	if selAt >= h.top+listH {
		h.top = selAt - listH + 1
	}
	if h.sel == 0 {
		h.top = 0
	}
	h.top = min(h.top, max(0, len(lines)-listH))
	for _, l := range lines[h.top:min(len(lines), h.top+listH)] {
		rows = append(rows, ansi.Truncate(l, inner, "…"))
	}
	hint := "type to filter · ↑ ↓ move · enter on a command starts it · esc close"
	if len(lines) > listH {
		hint = fmt.Sprintf("%d of %d · ", min(h.sel+1, len(h.matches)), len(h.matches)) + hint
	}
	rows = append(rows, "", sDim.Render(ansi.Truncate(hint, inner, "…")))
	return sBox.Width(boxW - 2).Render(strings.Join(rows, "\n"))
}
