package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// picker is the emoji chooser for reactions.
type picker struct {
	input   textinput.Model
	conv    string
	msg     backend.Message
	names   []string // every candidate
	matches []string
	sel     int
}

// commonReactions open the picker before anything is typed.
var commonReactions = []string{"+1", "heart", "joy", "eyes", "white_check_mark", "tada", "pray", "fire", "raised_hands", "100", "thinking_face", "rocket"}

const pickerRows = 10

type reactedMsg struct {
	ev  backend.EvReaction
	err error
}

func (m *Model) newPicker(conv string, msg backend.Message) *picker {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.Placeholder = "react with…"
	ti.Focus()
	p := &picker{input: ti, conv: conv, msg: msg}
	p.names = render.EmojiNames()
	for n := range m.emoji {
		p.names = append(p.names, n)
	}
	slices.Sort(p.names)
	p.names = slices.Compact(p.names)
	p.filter()
	return p
}

func (p *picker) filter() {
	q := strings.ToLower(strings.Trim(strings.TrimSpace(p.input.Value()), ":"))
	p.matches = p.matches[:0]
	p.sel = 0
	if q == "" {
		// Reactions already on the message first, then the usual ones.
		for _, r := range p.msg.Reactions {
			p.matches = append(p.matches, r.Name)
		}
		for _, n := range commonReactions {
			if !slices.Contains(p.matches, n) {
				p.matches = append(p.matches, n)
			}
		}
		return
	}
	for _, i := range fuzzyRank(q, p.names) {
		p.matches = append(p.matches, p.names[i])
		if len(p.matches) == 200 {
			break
		}
	}
}

// visible is the slice of matches on screen, so their images get loaded.
func (p *picker) visible() []string {
	start := max(0, p.sel-pickerRows+1)
	return p.matches[start:min(len(p.matches), start+pickerRows)]
}

func (m *Model) openPicker(conv string, msg backend.Message) tea.Cmd {
	m.picker = m.newPicker(conv, msg)
	return nil
}

func (m *Model) pickerKey(k tea.KeyMsg) tea.Cmd {
	p := m.picker
	switch k.String() {
	case "esc":
		m.picker = nil
		return nil
	case "enter":
		m.picker = nil
		if len(p.matches) == 0 {
			return nil
		}
		return m.toggleReaction(p.conv, p.msg.TS, p.matches[p.sel])
	case "down", "ctrl+n", "ctrl+j", "tab":
		p.sel = min(p.sel+1, max(len(p.matches)-1, 0))
		return nil
	case "up", "ctrl+p", "shift+tab":
		p.sel = max(p.sel-1, 0)
		return nil
	}
	var cmd tea.Cmd
	before := p.input.Value()
	p.input, cmd = p.input.Update(k)
	if p.input.Value() != before {
		p.filter()
	}
	return cmd
}

// hasReacted reports whether we already reacted to ts with name.
func (m *Model) hasReacted(conv, ts, name string) bool {
	for _, l := range []*msgList{m.lists[conv], m.threadList(conv)} {
		if l == nil {
			continue
		}
		for _, msg := range l.msgs {
			if msg.TS != ts {
				continue
			}
			for _, r := range msg.Reactions {
				if r.Name == name && slices.Contains(r.Users, m.client.SelfID) {
					return true
				}
			}
			return false
		}
	}
	return false
}

func (m *Model) threadList(conv string) *msgList {
	if m.thread != nil && m.thread.conv == conv {
		return &m.thread.list
	}
	return nil
}

// toggleReaction adds the reaction, or removes it if we already have it.
// The change shows at once; the live event that follows is a no-op, and a
// failed call puts it back.
func (m *Model) toggleReaction(conv, ts, name string) tea.Cmd {
	ev := backend.EvReaction{Channel: conv, TS: ts, User: m.client.SelfID, Name: name, Added: !m.hasReacted(conv, ts, name)}
	m.applyReaction(ev)
	return func() tea.Msg {
		return reactedMsg{ev: ev, err: m.client.React(m.ctx, conv, ts, name, ev.Added)}
	}
}

func (m *Model) renderPicker(width, height int) string {
	p := m.picker
	boxW := min(50, width-4)
	inner := boxW - 4
	p.input.Width = inner - 3
	rows := []string{p.input.View(), sFaint.Render(strings.Repeat("─", inner))}
	start := max(0, p.sel-pickerRows+1)
	for i := start; i < len(p.matches) && i < start+pickerRows; i++ {
		name := p.matches[i]
		label := m.emojiChip(name) + "  :" + name + ":"
		if m.hasReacted(p.conv, p.msg.TS, name) {
			label += sOK.Render("  ✓")
		}
		if i == p.sel {
			rows = append(rows, sBar.Render("▌")+" "+label)
		} else {
			rows = append(rows, "  "+label)
		}
	}
	if len(p.matches) == 0 {
		rows = append(rows, sDim.Render("  no emoji match"))
	}
	rows = append(rows, "", sFaint.Render("enter react / un-react · esc close"))
	return sBox.Width(boxW - 2).Render(strings.Join(rows, "\n"))
}
