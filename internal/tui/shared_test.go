package tui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/kitty"
)

func TestSharedView(t *testing.T) {
	m := fakeModel(t, 110, 30)
	m.Update(key("F"))
	if m.shared != nil || m.status != "open a conversation first" {
		t.Fatal("F in Unreads should say to open a conversation")
	}
	m.open("C1")
	m.Update(key("F"))
	s := m.shared
	if s == nil || !s.tabs[tabFiles].loading || !s.tabs[tabLinks].loading {
		t.Fatal("F should open the pane and load both tabs")
	}
	for _, k := range []string{"enter", "o", "c", "G"} { // nothing to act on yet
		m.Update(key(k))
	}
	created := time.Date(2026, 3, 2, 14, 5, 0, 0, time.Local)
	m.Update(convFilesMsg{conv: "C1", res: backend.FilesPage{Total: 2, Page: 1, Pages: 1, Files: []backend.SharedFile{
		{File: backend.File{ID: "F1", Name: "plan.pdf", Title: "Launch plan"}, User: "U1", Created: created, Size: 1_260_000, Kind: "PDF"},
		{File: backend.File{ID: "F2", Name: "tiny.txt"}, User: "U2", Created: created, Size: 820},
	}}})
	m.Update(convLinksMsg{conv: "C1", res: backend.SearchPage{Total: 41, Page: 1, Pages: 2, Hits: []backend.Hit{
		hit("C1", "300.000001", "", "U2", "see <https://example.com/a|the spec> and <https://example.org/b>"),
		hit("C1", "250.0", "200.0", "U1", "https <https://example.net/c>"),
	}}})
	v := dump(t, m)
	for _, want := range []string{"Files & links", " Files 2 ", " Links 3+ ", "Launch plan  PDF · 1.3 MB", "Anna Jensen 2 Mar", "tiny.txt  820 B"} {
		if !strings.Contains(v, want) {
			t.Errorf("files tab: missing %q", want)
		}
	}
	m.Update(key("tab"))
	v = dump(t, m)
	for _, want := range []string{"the spec", "https://example.com/a", "https://example.org/b", "https://example.net/c", "in thread"} {
		if !strings.Contains(v, want) {
			t.Errorf("links tab: missing %q", want)
		}
	}
	if strings.Count(v, "https://example.org/b") != 1 {
		t.Error("a bare link should not be shown twice")
	}

	// Near the end of the links: the next page.
	m.Update(key("j"))
	if !s.tabs[tabLinks].loading {
		t.Error("moving near the end should fetch more links")
	}
	m.Update(convLinksMsg{conv: "C1", res: backend.SearchPage{Total: 41, Page: 2, Pages: 2, Hits: []backend.Hit{
		hit("C1", "100.0", "", "U1", "<https://example.com/old>"),
	}}})
	if len(s.links) != 4 || !strings.Contains(dump(t, m), " Links 4 ") {
		t.Errorf("%d links after page 2", len(s.links))
	}

	// enter on a link goes to its message (in its thread).
	m.Update(key("G"))
	m.Update(key("k"))
	m.Update(key("enter"))
	if m.shared != nil || m.jump == nil || m.jump.thread != "200.0" || m.jump.ts != "250.0" {
		t.Errorf("enter on a link: jump %+v", m.jump)
	}
}

func TestSharedFileGoTo(t *testing.T) {
	m := fakeModel(t, 110, 30)
	m.open("C1")
	m.Update(key("F"))
	m.Update(convFilesMsg{conv: "C1", res: backend.FilesPage{Total: 1, Page: 1, Pages: 1, Files: []backend.SharedFile{
		{File: backend.File{ID: "F1", Name: "plan.pdf"}, User: "U1"},
	}}})
	m.Update(fileShareMsg{conv: "C1", err: errors.New("files.info: no message shares it here")})
	if m.shared == nil || !m.statusErr {
		t.Error("a failed lookup should stay in the pane and say why")
	}
	m.Update(fileShareMsg{conv: "C1", ts: "150.0"})
	if m.shared != nil || m.jump == nil || m.jump.ts != "150.0" || m.jump.thread != "" {
		t.Errorf("jump %+v", m.jump)
	}
	// An answer for a pane that was closed is ignored.
	m.jump = nil
	m.Update(fileShareMsg{conv: "C1", ts: "150.0"})
	if m.jump != nil {
		t.Error("answer after closing should do nothing")
	}
}

func TestFmtSize(t *testing.T) {
	for n, want := range map[int]string{0: "0 B", 999: "999 B", 14_400: "14 KB", 3_210_000: "3.2 MB", 2_000_000_000: "2.0 GB"} {
		if got := fmtSize(n); got != want {
			t.Errorf("%d: %q, want %q", n, got, want)
		}
	}
}

func TestSharedThumbs(t *testing.T) {
	m := fakeModel(t, 110, 30)
	tty, _ := os.CreateTemp(t.TempDir(), "tty")
	m.SetGraphics(kitty.NewWriter(tty, false), "")
	m.open("C1")
	m.Update(key("F"))
	png := func(id string) backend.SharedFile {
		return backend.SharedFile{File: backend.File{ID: id, Name: "image.png", Mimetype: "image/png", Thumb: "https://files.slack.com/" + id}, User: "U1"}
	}
	m.Update(convFilesMsg{conv: "C1", res: backend.FilesPage{Total: 3, Page: 1, Pages: 1, Files: []backend.SharedFile{
		png("I1"), {File: backend.File{ID: "T1", Name: "notes.txt"}, User: "U2"}, png("I2"),
	}}})
	if m.images[thumbKey("I1")] == nil || m.images[thumbKey("I2")] == nil || m.images[thumbKey("T1")] != nil {
		t.Fatal("previews should be fetched for the images only")
	}
	m.Update(imageMsg{fileID: thumbKey("I1"), cols: 12, rows: 3, png: []byte("x")})
	v := dump(t, m)
	if n := strings.Count(v, "\U0010EEEE"); n < 3*12 {
		t.Errorf("preview not drawn: %d placeholder cells", n)
	}
	if strings.Contains(v, picture) {
		t.Error("layout marker leaked")
	}

	// v opens the viewer on the images, from the selected one on.
	m.Update(key("j")) // notes.txt: the next image is I2
	m.Update(key("v"))
	if m.viewer == nil || len(m.viewer.items) != 2 || m.viewer.items[m.viewer.idx].file.ID != "I2" {
		t.Fatalf("viewer %+v", m.viewer)
	}
}
