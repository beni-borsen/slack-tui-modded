package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/kitty"
)

// viewer shows one image at a time over the whole window, and steps through
// every image in the conversation (or thread) it was opened from.
type viewer struct {
	items []viewItem
	idx   int
	list  *msgList
	id    uint32 // kitty image id, reused for each picture
	shown string // file id the terminal holds under id
	cache map[string]*viewImg
	w, h  int // the box everything in cache was fitted to
}

type viewItem struct {
	file backend.File
	ts   string // message it belongs to, to put the cursor there on close
}

type viewImg struct {
	loading    bool
	err        string
	png        []byte
	cols, rows int
}

type viewLoadedMsg struct {
	fileID     string
	w, h       int
	png        []byte
	cols, rows int
	err        error
}

// openViewer starts at the selected message's image, or the nearest one
// above it.
func (m *Model) openViewer(l *msgList, sel int) tea.Cmd {
	var items []viewItem
	start := -1
	for i, msg := range l.msgs {
		for _, f := range msg.Files {
			if !f.IsImage() {
				continue
			}
			if i <= sel && (start < 0 || l.msgs[i].TS != items[start].ts) {
				start = len(items)
			}
			items = append(items, viewItem{file: f, ts: msg.TS})
		}
	}
	if len(items) == 0 {
		return m.setStatus("no images here", false)
	}
	if start < 0 {
		start = 0
	}
	if m.gfx == nil {
		f := items[start].file
		return tea.Batch(m.openFile(f), m.setStatus("no inline images ("+m.gfxReason+"), opening "+f.Name+"…", false))
	}
	m.viewer = &viewer{items: items, idx: start, list: l, id: m.newImageID(), cache: map[string]*viewImg{}}
	return m.loadViewer()
}

// viewBox is the picture's area: the whole window but the caption line.
func (m *Model) viewBox() (int, int) { return max(m.width, 1), max(m.height-1, 1) }

// loadViewer fetches the current image and its neighbours, so stepping is
// instant, and forgets the ones further away.
func (m *Model) loadViewer() tea.Cmd {
	v := m.viewer
	w, h := m.viewBox()
	if v.w != w || v.h != h {
		v.cache, v.shown, v.w, v.h = map[string]*viewImg{}, "", w, h
	}
	near := map[string]bool{}
	var cmds []tea.Cmd
	for _, i := range []int{v.idx, v.idx + 1, v.idx - 1} {
		if i < 0 || i >= len(v.items) {
			continue
		}
		f := v.items[i].file
		near[f.ID] = true
		if v.cache[f.ID] == nil {
			v.cache[f.ID] = &viewImg{loading: true}
			cmds = append(cmds, m.fetchFull(f, w, h))
		}
	}
	for id, st := range v.cache {
		if !near[id] && !st.loading {
			delete(v.cache, id)
		}
	}
	m.showViewer()
	return tea.Batch(cmds...)
}

func (m *Model) fetchFull(f backend.File, w, h int) tea.Cmd {
	cw, ch := m.cellW, m.cellH
	return func() tea.Msg {
		m.imgSem <- struct{}{}
		defer func() { <-m.imgSem }()
		path, err := m.cachedFile(f)
		if err != nil {
			return viewLoadedMsg{fileID: f.ID, w: w, h: h, err: err}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return viewLoadedMsg{fileID: f.ID, w: w, h: h, err: err}
		}
		png, cols, rows, err := scaleImage(data, cw, ch, w, h, true)
		return viewLoadedMsg{fileID: f.ID, w: w, h: h, png: png, cols: cols, rows: rows, err: err}
	}
}

func (m *Model) viewerLoaded(msg viewLoadedMsg) {
	v := m.viewer
	if v == nil || msg.w != v.w || msg.h != v.h {
		return // closed, or fitted to a window size that is gone
	}
	st := &viewImg{png: msg.png, cols: msg.cols, rows: msg.rows}
	if msg.err != nil {
		st = &viewImg{err: msg.err.Error()}
	}
	v.cache[msg.fileID] = st
	m.showViewer()
}

// showViewer sends the current picture to the terminal if it isn't there
// yet. It runs in Update, so the data is out before the frame that prints
// its placeholders.
func (m *Model) showViewer() {
	v := m.viewer
	cur := v.items[v.idx].file.ID
	st := v.cache[cur]
	if st == nil || st.png == nil || v.shown == cur {
		return
	}
	if err := m.gfx.Show(v.id, st.png, st.cols, st.rows); err != nil {
		st.png, st.err = nil, err.Error()
		return
	}
	v.shown = cur
}

func (m *Model) viewerKey(k tea.KeyMsg) tea.Cmd {
	v := m.viewer
	switch k.String() {
	case "esc", "q", "v":
		m.closeViewer()
	case "left", "h", "k", "up":
		if v.idx > 0 {
			v.idx--
			return m.loadViewer()
		}
	case "right", "l", "j", "down", " ":
		if v.idx < len(v.items)-1 {
			v.idx++
			return m.loadViewer()
		}
	case "g", "home":
		v.idx = 0
		return m.loadViewer()
	case "G", "end":
		v.idx = len(v.items) - 1
		return m.loadViewer()
	case "o":
		f := v.items[v.idx].file
		return tea.Batch(m.openFile(f), m.setStatus("opening "+f.Name+"…", false))
	}
	return nil
}

// closeViewer frees the picture and leaves the cursor on the message of the
// last image looked at.
func (m *Model) closeViewer() {
	v := m.viewer
	m.viewer = nil
	m.gfx.Delete(v.id)
	ts := v.items[v.idx].ts
	for i, msg := range v.list.msgs {
		if msg.TS == ts {
			if i == len(v.list.msgs)-1 {
				v.list.sel = -1
			} else {
				v.list.sel = i
			}
			break
		}
	}
}

func (m *Model) renderViewer() string {
	v := m.viewer
	w, h := m.viewBox()
	f := v.items[v.idx].file
	name := f.Title
	if name == "" {
		name = f.Name
	}
	var body string
	switch st := v.cache[f.ID]; {
	case st == nil || st.loading:
		body = sDim.Render("loading " + name + "…")
	case st.err != "":
		body = sErr.Render(name + ": " + st.err)
	case v.shown != f.ID:
		body = sDim.Render("loading " + name + "…")
	default:
		body = strings.Join(kitty.Placeholder(v.id, st.cols, st.rows), "\n")
	}
	pic := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, body)
	caption := sDim.Render(fmt.Sprintf("  %s · %d/%d · ", name, v.idx+1, len(v.items))) +
		sKey.Render("← →") + sDim.Render(" browse · ") + sKey.Render("o") + sDim.Render(" open · ") +
		sKey.Render("esc") + sDim.Render(" close")
	return fit(pic, w, h) + "\n" + fit(caption, w, 1)
}
