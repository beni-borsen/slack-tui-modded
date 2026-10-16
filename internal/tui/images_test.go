package tui

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/kitty"
)

func TestFitCells(t *testing.T) {
	for _, tc := range []struct{ w, h, cols, rows int }{
		{1600, 900, 40, 10}, // wide screenshot: width-limited
		{900, 1600, 15, 12}, // tall photo: height-limited
		{64, 64, 8, 4},      // small icon: not enlarged past its own width
		{4000, 100, 40, 1},  // banner
	} {
		c, r := fitCells(tc.w, tc.h, 8, 18, 40, 12)
		if c != tc.cols || r != tc.rows {
			t.Errorf("fitCells(%d,%d) = %dx%d, want %dx%d", tc.w, tc.h, c, r, tc.cols, tc.rows)
		}
	}
}

func TestImageInMessage(t *testing.T) {
	m := fakeModel(t, 100, 30)
	f, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatal(err)
	}
	m.SetGraphics(kitty.NewWriter(f, true), "")
	m.open("C2")
	shot := backend.File{ID: "F1", Name: "shot.png", Mimetype: "image/png", Thumb: "https://files.slack.com/t.png"}
	m.update(historyMsg{id: "C2", msgs: []backend.Message{{TS: "100.000001", User: "U1", Text: "look", Files: []backend.File{shot}}}})

	// The model asks for the image once and shows it as loading.
	m.scanImages()
	if st := m.images["F1"]; st == nil || st.status != imgLoading {
		t.Fatalf("image not requested: %+v", st)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "shot.png · loading…") {
		t.Error("no loading label")
	}
	// Once ready, its placeholders are in the view.
	m.update(imageMsg{fileID: "F1", cols: 10, rows: 3, png: []byte("x")})
	if n := strings.Count(m.View(), "\U0010EEEE"); n != 30 {
		t.Errorf("view has %d placeholder cells, want 10x3", n)
	}
	// Focus resends it through the tmux wrapper.
	if cmd := m.resendImages(); cmd != nil {
		cmd()
	}
	b, _ := os.ReadFile(f.Name())
	if !strings.Contains(string(b), "\x1bPtmux;\x1b\x1b_Ga=T,U=1,f=100") {
		t.Errorf("resend wrote %q", b)
	}
	// A failed download says why instead of breaking the layout.
	m.update(imageMsg{fileID: "F1", err: errors.New("403 Forbidden")})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "403 Forbidden") {
		t.Error("no error label")
	}
}
