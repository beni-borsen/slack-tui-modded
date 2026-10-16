package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// opener chooses what `o` opens when a message has several files or links.
type opener struct {
	items []openItem
	sel   int
}

type openItem struct {
	file *backend.File // a file, or
	link render.Link   // a web link
}

const openerRows = 9

// openFrom opens a message's only file or link, or lets you choose.
func (m *Model) openFrom(msg backend.Message) tea.Cmd {
	var items []openItem
	for i := range msg.Files {
		items = append(items, openItem{file: &msg.Files[i]})
	}
	for _, l := range render.Links(msg.Text) {
		items = append(items, openItem{link: l})
	}
	switch len(items) {
	case 0:
		return m.setStatus("no file or link on this message", false)
	case 1:
		return m.openItem(items[0])
	}
	m.opener = &opener{items: items}
	return nil
}

func (m *Model) openItem(it openItem) tea.Cmd {
	if f := it.file; f != nil {
		return tea.Batch(m.openFile(*f), m.setStatus("opening "+f.Name+"…", false))
	}
	return tea.Batch(m.openURL(it.link.URL), m.setStatus("opening "+it.link.URL, false))
}

func (m *Model) openerKey(k tea.KeyMsg) tea.Cmd {
	o := m.opener
	switch s := k.String(); s {
	case "esc", "q", "o":
		m.opener = nil
	case "j", "down", "tab":
		o.sel = (o.sel + 1) % len(o.items)
	case "k", "up", "shift+tab":
		o.sel = (o.sel - 1 + len(o.items)) % len(o.items)
	case "enter", "l":
		m.opener = nil
		return m.openItem(o.items[o.sel])
	default:
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			if i := int(s[0] - '1'); i < len(o.items) {
				m.opener = nil
				return m.openItem(o.items[i])
			}
		}
	}
	return nil
}

func (m *Model) renderOpener(width int) string {
	o := m.opener
	boxW := min(70, width-4)
	inner := boxW - 4
	rows := []string{sTitle.Render("Open"), sFaint.Render(strings.Repeat("─", inner))}
	start := max(0, o.sel-openerRows+1)
	for i := start; i < len(o.items) && i < start+openerRows; i++ {
		it := o.items[i]
		num := "  "
		if i < 9 {
			num = sKey.Render(fmt.Sprint(i+1)) + " "
		}
		var label string
		if f := it.file; f != nil {
			name := f.Title
			if name == "" {
				name = f.Name
			}
			kind := "file"
			if f.IsImage() {
				kind = "image"
			}
			label = name + "  " + sDim.Render(kind)
		} else if it.link.Label != "" {
			label = it.link.Label + "  " + sDim.Render(it.link.URL)
		} else {
			label = it.link.URL
		}
		label = ansi.Truncate(label, inner-4, "…")
		if i == o.sel {
			rows = append(rows, sBar.Render("▌")+" "+num+label)
		} else {
			rows = append(rows, "  "+num+label)
		}
	}
	rows = append(rows, "", sFaint.Render("1-9 or enter open · j k move · esc close"))
	return sBox.Width(boxW - 2).Render(strings.Join(rows, "\n"))
}
