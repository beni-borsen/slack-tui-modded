package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/kitty"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// sharedState is `F`: the files and the links shared in a conversation, on
// two tabs. Files come from files.list, links from a has:link search (so
// they reach back through all of the history, not just what is loaded).
// It takes over the main pane like pins.
type sharedState struct {
	conv string
	tab  int // tabFiles or tabLinks
	tabs [2]sharedTab

	files []backend.SharedFile
	links []linkRow
}

type sharedTab struct {
	page, pages, total int
	loading            bool
	err                string
	sel, top           int
}

const (
	tabFiles = iota
	tabLinks
)

// Image previews in the Files tab: most pasted images are all called
// image.png, so the picture is what tells them apart.
const thumbCols, thumbRows = 24, 6

func thumbKey(fileID string) string { return "thumb/" + fileID }

// linkRow is one link; a message with several links gives several rows.
type linkRow struct {
	link render.Link
	hit  backend.Hit
}

type convFilesMsg struct {
	conv string
	res  backend.FilesPage
	err  error
}

type convLinksMsg struct {
	conv string
	res  backend.SearchPage
	err  error
}

type fileShareMsg struct {
	conv         string
	ts, threadTS string
	err          error
}

func (m *Model) openShared() tea.Cmd {
	if m.current == unreadsID || m.convs[m.current] == nil {
		return m.setStatus("open a conversation first", false)
	}
	m.closeSearch()
	m.pins, m.sched, m.chans = nil, nil, nil
	m.shared = &sharedState{conv: m.current}
	return tea.Batch(m.loadSharedFiles(1), m.loadSharedLinks(1))
}

func (m *Model) loadSharedFiles(page int) tea.Cmd {
	s := m.shared
	s.tabs[tabFiles].loading = true
	conv := s.conv
	return func() tea.Msg {
		res, err := m.client.ConvFiles(m.ctx, conv, page)
		return convFilesMsg{conv, res, err}
	}
}

func (m *Model) loadSharedLinks(page int) tea.Cmd {
	s := m.shared
	s.tabs[tabLinks].loading = true
	conv := s.conv
	return func() tea.Msg {
		res, err := m.client.Search(m.ctx, "in:<#"+conv+"> has:link", page)
		return convLinksMsg{conv, res, err}
	}
}

func (m *Model) sharedFilesResult(msg convFilesMsg) tea.Cmd {
	s := m.shared
	if s == nil || s.conv != msg.conv {
		return nil
	}
	t := &s.tabs[tabFiles]
	t.loading = false
	if msg.err != nil {
		t.err = msg.err.Error()
		return nil
	}
	s.files = append(s.files, msg.res.Files...)
	t.page, t.pages, t.total = msg.res.Page, msg.res.Pages, msg.res.Total
	return m.scanThumbs()
}

// scanThumbs fetches previews for the images around the cursor.
func (m *Model) scanThumbs() tea.Cmd {
	s := m.shared
	if m.gfx == nil || s == nil || s.tab != tabFiles {
		return nil
	}
	sel := s.tabs[tabFiles].sel
	var cmds []tea.Cmd
	for i := max(sel-5, 0); i < min(sel+15, len(s.files)); i++ {
		f := s.files[i].File
		if !f.IsImage() || m.images[thumbKey(f.ID)] != nil {
			continue
		}
		st := &imgState{id: m.newImageID()}
		m.images[thumbKey(f.ID)] = st
		cmds = append(cmds, m.fetchImageSized(thumbKey(f.ID), f, st.id, thumbCols, thumbRows))
	}
	return tea.Batch(cmds...)
}

// viewSharedImages opens the full-window viewer on the Files tab's images,
// starting at the selected one (or the next image after it).
func (m *Model) viewSharedImages() tea.Cmd {
	s := m.shared
	l := &msgList{sel: -1}
	start := 0
	for i, f := range s.files {
		if !f.IsImage() {
			continue
		}
		if i < s.tabs[tabFiles].sel {
			start++
		}
		l.msgs = append(l.msgs, backend.Message{TS: strconv.Itoa(i), Files: []backend.File{f.File}})
	}
	if len(l.msgs) == 0 {
		return m.setStatus("no images here", false)
	}
	return m.openViewer(l, min(start, len(l.msgs)-1))
}

func (m *Model) sharedLinksResult(msg convLinksMsg) {
	s := m.shared
	if s == nil || s.conv != msg.conv {
		return
	}
	t := &s.tabs[tabLinks]
	t.loading = false
	if msg.err != nil {
		t.err = msg.err.Error()
		return
	}
	for _, h := range msg.res.Hits {
		text := strings.NewReplacer(backend.HitStart, "", backend.HitEnd, "").Replace(h.Text)
		for _, l := range render.Links(text) {
			s.links = append(s.links, linkRow{link: l, hit: h})
		}
	}
	t.page, t.pages, t.total = msg.res.Page, msg.res.Pages, msg.res.Total
}

func (s *sharedState) count() int {
	if s.tab == tabFiles {
		return len(s.files)
	}
	return len(s.links)
}

func (m *Model) sharedKey(k tea.KeyMsg) tea.Cmd {
	s := m.shared
	t := &s.tabs[s.tab]
	n := s.count()
	switch k.String() {
	case "esc", "F":
		m.shared = nil
		return nil
	case "q":
		return m.quit()
	case "/":
		m.shared = nil
		return m.openSearch()
	case "ctrl+k":
		m.shared = nil
		m.switcher = m.newSwitcher()
		return nil
	case "tab", "shift+tab", "h", "l", "left", "right":
		s.tab = 1 - s.tab
		return m.scanThumbs()
	case "v":
		if s.tab == tabFiles {
			return m.viewSharedImages()
		}
		return nil
	case "j", "down":
		t.sel++
	case "k", "up":
		t.sel--
	case "ctrl+d":
		t.sel += 5
	case "ctrl+u":
		t.sel -= 5
	case "g", "home":
		t.sel = 0
	case "G", "end":
		t.sel = n - 1
	case "enter":
		if n == 0 {
			return nil
		}
		if s.tab == tabLinks {
			h := s.links[min(t.sel, n-1)].hit
			m.shared = nil
			return m.goToMessage(h.Channel, h.Message)
		}
		f := s.files[min(t.sel, n-1)]
		conv := s.conv
		return tea.Batch(m.setStatus("finding the message…", false), func() tea.Msg {
			ts, thread, err := m.client.FileShare(m.ctx, f.ID, conv)
			return fileShareMsg{conv: conv, ts: ts, threadTS: thread, err: err}
		})
	case "o":
		if n == 0 {
			return nil
		}
		if s.tab == tabLinks {
			l := s.links[min(t.sel, n-1)].link
			return tea.Batch(m.openURL(l.URL), m.setStatus("opening "+l.URL, false))
		}
		f := s.files[min(t.sel, n-1)].File
		return tea.Batch(m.openFile(f), m.setStatus("opening "+f.Name+"…", false))
	case "c":
		if n == 0 {
			return nil
		}
		if s.tab == tabLinks {
			return m.copyText(s.links[min(t.sel, n-1)].link.URL)
		}
		return m.copyText(s.files[min(t.sel, n-1)].Permalink)
	}
	t.sel = min(max(t.sel, 0), max(n-1, 0))
	thumbs := m.scanThumbs()
	// Near the end: fetch the next page.
	if n > 0 && t.sel >= n-3 && t.page < t.pages && !t.loading {
		if s.tab == tabFiles {
			return tea.Batch(thumbs, m.loadSharedFiles(t.page+1))
		}
		return m.loadSharedLinks(t.page + 1)
	}
	return thumbs
}

func (m *Model) fileShareResult(msg fileShareMsg) tea.Cmd {
	s := m.shared
	if s == nil || s.conv != msg.conv {
		return nil // closed while looking
	}
	if msg.err != nil {
		return m.setStatus(msg.err.Error(), true)
	}
	m.shared = nil
	m.status = ""
	return m.goToMessage(msg.conv, backend.Message{TS: msg.ts, ThreadTS: msg.threadTS})
}

// fmtSize is a file size the way people say it: 820 B, 14 KB, 3.2 MB.
func fmtSize(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 1000*1000:
		return fmt.Sprintf("%d KB", (n+500)/1000)
	case n < 1000*1000*1000:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	}
	return fmt.Sprintf("%.1f GB", float64(n)/1e9)
}

func (m *Model) renderShared(w, h int) string {
	s := m.shared
	title := sTitle.Render("Files & links")
	if c := m.convs[s.conv]; c != nil {
		title += sDim.Render("  " + m.convTitle(c))
	}
	var tabs []string
	for i, name := range []string{"Files", "Links"} {
		t := s.tabs[i]
		label := name
		switch {
		case i == tabFiles && t.pages > 0:
			label += fmt.Sprintf(" %d", t.total)
		case i == tabLinks && t.pages > 0:
			label += fmt.Sprintf(" %d", len(s.links))
			if t.page < t.pages {
				label += "+"
			}
		}
		if i == s.tab {
			tabs = append(tabs, sActive.Render(" "+label+" "))
		} else {
			tabs = append(tabs, sDim.Render(" "+label+" "))
		}
	}
	header := []string{title, " " + strings.Join(tabs, " "), rule(w, true)}
	footer := ansi.Truncate(sFaint.Render("  ")+sKey.Render("enter")+sFaint.Render(" go to message  ")+
		sKey.Render("o")+sFaint.Render(" open  ")+sKey.Render("v")+sFaint.Render(" view images  ")+sKey.Render("c")+sFaint.Render(" copy link  ")+
		sKey.Render("tab")+sFaint.Render(" files / links  ")+sKey.Render("esc")+sFaint.Render(" close"), w, "…")
	t := &s.tabs[s.tab]
	if t.err != "" {
		header = append(header, "  "+sErr.Render(t.err))
	}
	bodyH := max(1, h-len(header)-2)
	n := s.count()
	if n == 0 {
		msg := "no files shared here"
		if s.tab == tabLinks {
			msg = "no links shared here"
		}
		if t.loading {
			msg = "loading…"
		}
		return strings.Join(append(header, placeholder(msg, w, bodyH), rule(w, false), footer), "\n")
	}

	sel := min(max(t.sel, 0), n-1)
	var all []string
	var selStart, selEnd int
	for i := range n {
		var block []string
		if s.tab == tabFiles {
			f := s.files[i]
			name := f.Title
			if name == "" {
				name = f.Name
			}
			info := []string{}
			if f.Kind != "" {
				info = append(info, f.Kind)
			}
			if f.Size > 0 {
				info = append(info, fmtSize(f.Size))
			}
			line := sNormal.Render(name)
			if len(info) > 0 {
				line += sDim.Render("  " + strings.Join(info, " · "))
			}
			by := backend.Message{User: f.User}
			block = []string{line}
			if st := m.images[thumbKey(f.ID)]; st != nil && st.status == imgReady {
				for _, row := range kitty.Placeholder(st.id, st.cols, st.rows) {
					block = append(block, picture+row)
				}
			}
			block = append(block, nameStyle(senderKey(by)).Render(m.senderName(by))+" "+sTime.Render(relTime(f.Created, m.cfg.TimeFormat)))
		} else {
			r := s.links[i]
			label := r.link.Label
			if label == "" {
				label = r.link.URL
			}
			msg := r.hit.Message
			sub := nameStyle(senderKey(msg)).Render(m.senderName(msg)) + " " + sTime.Render(relTime(msg.Time(), m.cfg.TimeFormat))
			if msg.IsReply() {
				sub += sThread.Render("  in thread")
			}
			block = []string{sNormal.Render(label)}
			if label != r.link.URL {
				block = append(block, sDim.Render(r.link.URL))
			}
			block = append(block, sub)
		}
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
	if t.loading {
		all = append(all, "  "+sFaint.Render("loading more…"))
	}

	if selStart < t.top {
		t.top = selStart
	}
	if selEnd > t.top+bodyH {
		t.top = max(selStart, selEnd-bodyH)
	}
	t.top = min(max(t.top, 0), max(0, len(all)-bodyH))
	view := all[t.top:min(len(all), t.top+bodyH)]
	for len(view) < bodyH {
		view = append(view, "")
	}
	for i, l := range view {
		if strings.Contains(l, picture) {
			view[i] = strings.Replace(l, picture, "", 1) // narrower than the pane; truncating would split its cells
		} else {
			view[i] = ansi.Truncate(l, w, "…")
		}
	}
	return strings.Join(append(append(header, view...), rule(w, false), footer), "\n")
}

// picture marks a preview row while the pane is laid out.
const picture = "\x00pic\x00"
