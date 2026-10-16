package tui

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/kitty"
)

// run executes a command and feeds what it returns back in, batches too.
func run(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			run(m, c)
		}
	case nil:
	default:
		_, next := m.Update(msg)
		_ = next // follow-up commands (scans, ticks) aren't part of this
	}
}

func TestImageViewer(t *testing.T) {
	m := chanWithMessages(t)
	tty, _ := os.CreateTemp(t.TempDir(), "tty")
	m.SetGraphics(kitty.NewWriter(tty, false), "")
	m.cellW, m.cellH = 8, 16

	// Originals already in the download cache, so nothing hits the network.
	dir := filepath.Join(cacheDir(), "files")
	os.MkdirAll(dir, 0o700)
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 40, 20)))
	img := func(id string) backend.File {
		os.WriteFile(filepath.Join(dir, id+"-"+id+".png"), buf.Bytes(), 0o600)
		return backend.File{ID: id, Name: id + ".png", Mimetype: "image/png", URL: "https://files.slack.com/" + id}
	}
	m.update(historyMsg{id: "C2", msgs: []backend.Message{
		{TS: "100.000001", User: "U1", Files: []backend.File{img("A1"), img("A2")}},
		{TS: "100.000002", User: "ME", Text: "nice"},
		{TS: "100.000003", User: "U1", Files: []backend.File{img("C1")}},
	}})
	m.focus = fMessages

	_, cmd := m.Update(key("v"))
	run(m, cmd)
	v := m.viewer
	if v == nil || v.items[v.idx].file.ID != "C1" {
		t.Fatalf("viewer should open on the newest image, got %+v", v)
	}
	if out := dump(t, m); !strings.Contains(out, "C1.png · 3/3") {
		t.Errorf("caption missing")
	}
	if v.shown != "C1" {
		t.Errorf("C1 not sent to the terminal (shown=%q)", v.shown)
	}
	// The picture fills the window: the 2:1 image is as wide as the 110 cols
	// allow at 8x16 cells (110x27 box), so 110 cols by 27.5 → 27 rows tall.
	if st := v.cache["C1"]; st.cols < 100 {
		t.Errorf("image not grown to the window: %dx%d", st.cols, st.rows)
	}

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	run(m, cmd)
	if v.items[v.idx].file.ID != "A2" || v.shown != "A2" {
		t.Errorf("left should step to A2, at %s shown %s", v.items[v.idx].file.ID, v.shown)
	}
	if !strings.Contains(ansi.Strip(m.View()), "A2.png · 2/3") {
		t.Error("caption did not follow")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.Update(tea.KeyMsg{Type: tea.KeyRight}) // past the end stays put
	if v.items[v.idx].file.ID != "C1" {
		t.Errorf("at %s, want C1", v.items[v.idx].file.ID)
	}
	m.Update(key("g"))
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.viewer != nil {
		t.Fatal("esc did not close")
	}
	if m.lists["C2"].sel != 0 {
		t.Errorf("cursor should land on the first message, sel=%d", m.lists["C2"].sel)
	}
}

func TestViewerNoImages(t *testing.T) {
	m := chanWithMessages(t)
	m.focus = fMessages
	m.Update(key("v"))
	if m.viewer != nil || !strings.Contains(m.status, "no images") {
		t.Errorf("viewer=%v status=%q", m.viewer, m.status)
	}
}

func TestPasteSeveralImages(t *testing.T) {
	fakeClipboard(t, `image/png\n`)
	m := chanWithMessages(t)
	m.Update(key("i"))
	for range 3 {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
		m.Update(cmd())
	}
	if len(m.attach) != 3 {
		t.Fatalf("%d attached, want 3", len(m.attach))
	}
	if out := dump(t, m); !strings.Contains(out, "3 images") {
		t.Error("bar should count the images")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if len(m.attach) != 2 {
		t.Errorf("ctrl+x should drop one, %d left", len(m.attach))
	}
}
