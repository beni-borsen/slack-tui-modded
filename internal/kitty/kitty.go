// Package kitty draws images with the kitty graphics protocol using Unicode
// placeholders: an image is sent to the terminal once, then shows up
// wherever placeholder characters are printed. To the TUI those are ordinary
// one-cell characters, so images scroll and clip with the text around them,
// inside tmux panes too.
//
// Spec: https://sw.kovidgoyal.net/kitty/graphics-protocol/
package kitty

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

const placeholder = '\U0010EEEE'

// Support reports whether images can be shown, and why not when they can't.
func Support() (bool, string) {
	if os.Getenv("TMUX") != "" {
		name := tmuxValue("display-message", "-p", "#{client_termname}")
		if !graphicsTerm(name) {
			return false, "tmux client terminal " + name + " has no kitty graphics"
		}
		if p := tmuxValue("show-options", "-gv", "allow-passthrough"); p != "on" && p != "all" {
			return false, "tmux allow-passthrough is off"
		}
		return true, ""
	}
	if graphicsTerm(os.Getenv("TERM")) || os.Getenv("TERM_PROGRAM") == "ghostty" || os.Getenv("KITTY_WINDOW_ID") != "" {
		return true, ""
	}
	return false, "terminal " + os.Getenv("TERM") + " has no kitty graphics"
}

func graphicsTerm(name string) bool {
	return name == "xterm-ghostty" || name == "ghostty" || name == "xterm-kitty"
}

func tmuxValue(args ...string) string {
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SyncFile is the terminal with its writes serialised. The TUI draws
// through it and images are sent through it, so image data can never land
// in the middle of a frame (or a frame in the middle of image data), which
// corrupts the image. It is still a terminal file, so the TUI can put it in
// raw mode and read its size.
type SyncFile struct {
	*os.File
	mu sync.Mutex
}

func NewSyncFile(f *os.File) *SyncFile { return &SyncFile{File: f} }

func (f *SyncFile) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.File.Write(p)
}

func (f *SyncFile) WriteString(s string) (int, error) { return f.Write([]byte(s)) }

// Writer sends graphics commands to the terminal, one whole command
// sequence per Write.
type Writer struct {
	mu   sync.Mutex
	out  io.Writer
	tmux bool
	ids  map[uint32]bool
}

// NewWriter writes graphics commands to out: the same SyncFile the TUI
// draws to (tests use a plain buffer).
func NewWriter(out io.Writer, tmux bool) *Writer {
	return &Writer{out: out, tmux: tmux, ids: map[uint32]bool{}}
}

// InTmux reports whether commands need tmux passthrough wrapping.
func InTmux() bool { return os.Getenv("TMUX") != "" }

// command wraps one graphics command, for tmux passthrough when needed.
func (w *Writer) command(control, payload string) string {
	seq := "\x1b_G" + control
	if payload != "" {
		seq += ";" + payload
	}
	seq += "\x1b\\"
	if w.tmux {
		// tmux forwards DCS passthrough with every ESC doubled.
		seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return seq
}

// Show sends a PNG and makes it fill a cols×rows box wherever its
// placeholders are printed. q=2 keeps the terminal from replying, since
// replies would arrive on stdin as keystrokes.
func (w *Writer) Show(id uint32, png []byte, cols, rows int) error {
	if id == 0 || id >= 1<<24 {
		return errors.New("kitty: image id must be in 1..2^24-1")
	}
	data := base64.StdEncoding.EncodeToString(png)
	var b strings.Builder
	// Clear anything the terminal already holds under this id, with its
	// placements: sending new data alone can leave an old placement (with
	// its old box size) in charge of the id.
	b.WriteString(w.command(fmt.Sprintf("a=d,d=I,i=%d,q=2", id), ""))
	const chunk = 4096
	for i := 0; i < len(data); i += chunk {
		end := min(i+chunk, len(data))
		more := 0
		if end < len(data) {
			more = 1
		}
		control := fmt.Sprintf("m=%d", more)
		if i == 0 {
			control = fmt.Sprintf("a=T,U=1,f=100,t=d,i=%d,c=%d,r=%d,q=2,m=%d", id, cols, rows, more)
		}
		b.WriteString(w.command(control, data[i:end]))
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ids[id] = true
	_, err := io.WriteString(w.out, b.String())
	return err
}

// Delete frees one image in the terminal.
func (w *Writer) Delete(id uint32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ids[id] {
		delete(w.ids, id)
		io.WriteString(w.out, w.command(fmt.Sprintf("a=d,d=I,i=%d,q=2", id), ""))
	}
}

// Close frees every image this writer sent. Only ours: other programs in
// other tmux panes share the terminal's image store.
func (w *Writer) Close() {
	w.mu.Lock()
	var b strings.Builder
	for id := range w.ids {
		b.WriteString(w.command(fmt.Sprintf("a=d,d=I,i=%d,q=2", id), ""))
	}
	w.ids = map[uint32]bool{}
	io.WriteString(w.out, b.String())
	w.mu.Unlock()
}

// Inline returns one row of cols placeholder cells for image id, every
// cell spelling out its own row and column. Unlike Placeholder's rows this
// survives being split by line wrapping, so it suits emoji inside text.
func Inline(id uint32, cols int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", (id>>16)&0xff, (id>>8)&0xff, id&0xff)
	for c := range min(cols, len(diacritics)) {
		b.WriteRune(placeholder)
		b.WriteRune(diacritics[0])
		b.WriteRune(diacritics[c])
	}
	b.WriteString("\x1b[39m")
	return b.String()
}

// Placeholder returns the rows of text that draw image id in a cols×rows
// box. The image id travels in the foreground colour; the first cell of
// each row carries its row and column as combining marks, and the cells
// after it count on from there.
func Placeholder(id uint32, cols, rows int) []string {
	rows = min(rows, len(diacritics))
	cols = min(cols, len(diacritics))
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (id>>16)&0xff, (id>>8)&0xff, id&0xff)
	rest := strings.Repeat(string(placeholder), max(cols-1, 0))
	lines := make([]string, rows)
	for r := range rows {
		lines[r] = fg + string(placeholder) + string(diacritics[r]) + string(diacritics[0]) + rest + "\x1b[39m"
	}
	return lines
}
