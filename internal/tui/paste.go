package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

// attachment is a clipboard image waiting in the composer; enter uploads it
// with the typed text as its message.
type attachment struct {
	name string
	data []byte
}

// clipMsg is what a paste turned out to be: an image to attach, or the
// pasted text to insert after all.
type clipMsg struct {
	img  *attachment
	text string
	err  error
}

type uploadedMsg struct {
	atts []*attachment
	text string
	err  error
}

const maxAttach = 10 // Slack takes at most 10 files per message

const maxPaste = 50 << 20

// paste looks at the system clipboard before taking a terminal paste as text.
// A terminal can only paste text, so an image on the clipboard arrives as its
// raw bytes (or nothing); here it becomes an attachment instead. explicit is
// ctrl+v, which asks for an image and has no text to fall back to.
func (m *Model) paste(text string, explicit bool) tea.Cmd {
	return func() tea.Msg {
		types, err := clipTypes()
		if err != nil {
			if explicit {
				return clipMsg{err: err}
			}
			return clipMsg{text: text}
		}
		mime := pickImage(types)
		hasText := false
		for _, t := range types {
			if strings.HasPrefix(t, "text/plain") || t == "UTF8_STRING" || t == "STRING" {
				hasText = true
			}
		}
		// Prefer real text: copying from an office app offers a picture of
		// the text too. But binary junk in the paste means the image is it.
		if mime == "" || (!explicit && hasText && looksLikeText(text)) {
			if explicit {
				return clipMsg{err: errors.New("no image on the clipboard")}
			}
			return clipMsg{text: text}
		}
		data, err := clipRead(mime)
		if err != nil {
			return clipMsg{err: err}
		}
		ext := strings.TrimPrefix(mime, "image/")
		if ext == "jpeg" {
			ext = "jpg"
		}
		name := "pasted-" + time.Now().Format("2006-01-02-150405.000") + "." + ext
		return clipMsg{img: &attachment{name: name, data: data}}
	}
}

func looksLikeText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func pickImage(types []string) string {
	for _, want := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
		for _, t := range types {
			if t == want {
				return t
			}
		}
	}
	return ""
}

// clipTypes lists what the clipboard offers: wl-paste on Wayland, xclip on X.
func clipTypes() ([]string, error) {
	var cmd *exec.Cmd
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "":
		cmd = exec.Command("wl-paste", "--list-types")
	case os.Getenv("DISPLAY") != "":
		cmd = exec.Command("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
	default:
		return nil, errors.New("no display to read the clipboard from")
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reading clipboard: %w", err)
	}
	return strings.Fields(string(out)), nil
}

func clipRead(mime string) ([]byte, error) {
	var cmd *exec.Cmd
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		cmd = exec.Command("wl-paste", "--no-newline", "--type", mime)
	} else {
		cmd = exec.Command("xclip", "-selection", "clipboard", "-t", mime, "-o")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("reading clipboard: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(out, maxPaste+1))
	if len(data) > maxPaste {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, errors.New("clipboard image is over 50 MB")
	}
	if werr := cmd.Wait(); err == nil && werr != nil {
		err = fmt.Errorf("reading clipboard: %w", werr)
	}
	if err == nil && len(data) == 0 {
		err = errors.New("clipboard image is empty")
	}
	return data, err
}

func (m *Model) upload(id, threadTS string, atts []*attachment, text string) tea.Cmd {
	files := make([]backend.Upload, len(atts))
	for i, a := range atts {
		files[i] = backend.Upload{Name: a.name, Data: a.data}
	}
	return func() tea.Msg {
		err := m.client.Upload(m.ctx, id, threadTS, files, text)
		return uploadedMsg{atts: atts, text: text, err: err}
	}
}

// attachLabel is the bar above the composer while images wait there.
func (m *Model) attachLabel() string {
	switch len(m.attach) {
	case 0:
		return ""
	case 1:
		a := m.attach[0]
		return fmt.Sprintf(" %s · %s · enter send · ctrl+x remove ", a.name, humanSize(len(a.data)))
	}
	size := 0
	for _, a := range m.attach {
		size += len(a.data)
	}
	return fmt.Sprintf(" %d images · %s · enter send · ctrl+x remove last ", len(m.attach), humanSize(size))
}

func humanSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}
