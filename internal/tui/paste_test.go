package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeClipboard puts a stand-in wl-paste first on PATH, so tests never read
// (or depend on) the real clipboard.
func fakeClipboard(t *testing.T, types string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = --list-types ]; then printf '" + types + "'; else printf '\\211PNG\\r\\n\\032\\nDATA'; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "wl-paste"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
}

func pasteKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
}

func TestPasteImageAttaches(t *testing.T) {
	fakeClipboard(t, `image/png\n`)
	m := chanWithMessages(t)
	m.Update(key("i"))
	_, cmd := m.Update(pasteKey("\x89PNG\r\n\x1a\nDATA"))
	m.Update(cmd())
	if len(m.attach) != 1 || !strings.HasSuffix(m.attach[0].name, ".png") || string(m.attach[0].data[1:4]) != "PNG" {
		t.Fatalf("attach = %+v", m.attach)
	}
	if m.input.Value() != "" {
		t.Errorf("image bytes leaked into the input: %q", m.input.Value())
	}
	if v := dump(t, m); !strings.Contains(v, "ctrl+x remove") {
		t.Error("no attachment bar")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if len(m.attach) != 0 {
		t.Error("ctrl+x kept the image")
	}
}

func TestPasteTextStaysText(t *testing.T) {
	// A browser image copy also offers text; real text in the paste wins.
	fakeClipboard(t, `image/png\ntext/plain;charset=utf-8\n`)
	m := chanWithMessages(t)
	m.Update(key("i"))
	_, cmd := m.Update(pasteKey("hello there"))
	m.Update(cmd())
	if len(m.attach) != 0 || m.input.Value() != "hello there" {
		t.Fatalf("attach=%v input=%q", m.attach, m.input.Value())
	}
}

func TestCtrlVWithoutImage(t *testing.T) {
	fakeClipboard(t, `text/plain\n`)
	m := chanWithMessages(t)
	m.Update(key("i"))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	m.Update(cmd())
	if len(m.attach) != 0 || !strings.Contains(m.status, "no image") {
		t.Fatalf("attach=%v status=%q", m.attach, m.status)
	}
}
