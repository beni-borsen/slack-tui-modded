package tui

import (
	"encoding/base64"
	"io"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// SetClipboard sets where OSC 52 goes: the shared terminal writer.
func (m *Model) SetClipboard(w io.Writer) { m.clip = w }

// copyMessage puts a message's text on the clipboard with OSC 52, the same
// path Claude Code and vim use: tmux (set-clipboard on) passes it to the
// terminal, which sets the system clipboard. Works over ssh too.
func (m *Model) copyMessage(msg backend.Message) tea.Cmd {
	// Links as plain URLs, then @Name and #channel instead of <@U…>.
	text, _ := render.Editable(render.PlainLinks(msg.Text), m)
	text = strings.TrimSpace(text)
	if text == "" && len(msg.Files) > 0 {
		text = msg.Files[0].Permalink // a file-only message: copy its link
	}
	return m.copyText(text)
}

// copyText puts text on the clipboard (see copyMessage).
func (m *Model) copyText(text string) tea.Cmd {
	if text == "" {
		return m.setStatus("nothing to copy", false)
	}
	out := m.clip
	if out == nil {
		tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return m.setStatus("copy: "+err.Error(), true)
		}
		defer tty.Close()
		out = tty
	}
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
	if _, err := io.WriteString(out, seq); err != nil {
		return m.setStatus("copy: "+err.Error(), true)
	}
	n := len([]rune(text))
	return m.setStatus("copied "+plural(n, "1 character", strconv.Itoa(n)+" characters"), false)
}
