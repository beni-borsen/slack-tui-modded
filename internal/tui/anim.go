package tui

import (
	"bytes"
	"errors"
	"image"
	"image/gif"
	"image/png"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/image/draw"
)

// Animated GIFs. The terminal shows still pictures only (Ghostty doesn't do
// the kitty protocol's animation), so the frames are decoded and scaled
// once and all sent to the terminal, each under its own image id. A tick
// then only changes which id the view prints, for the GIFs on screen:
// nothing more is sent per frame.

const (
	maxViewAnimBytes = 48 << 20 // the full-window viewer's frames
	maxAnimFrames    = 200
	maxAnimPixels    = 100_000_000 // width × height × frames of the source
	maxAnimBytes     = 16 << 20    // all scaled frames, as PNG
)

type animFrames struct {
	frames [][]byte // PNG, all the same size
	delays []time.Duration
}

type animTick struct{}

var errNotAnimated = errors.New("not an animated GIF")

// scaleAnim decodes an animated GIF into frames fitted to at most
// maxCols×maxRows cells. errNotAnimated (or another error) means: show it
// as a still image.
func scaleAnim(data []byte, cw, ch, maxCols, maxRows int) (*animFrames, int, int, error) {
	return decodeAnim(data, cw, ch, maxCols, maxRows, maxAnimBytes)
}

// decodeAnim is scaleAnim with a limit on the frames' size.
//
// Frames are scaled to exactly their box. When the terminal has to scale a
// picture over its box, it does so cell by cell and the seams show as
// lines. So a GIF is never grown to fill the viewer's window, like still
// images are: at full-window size its frames are far too much to make and
// send (live: 150MB and 20s for a 142-frame GIF).
func decodeAnim(data []byte, cw, ch, maxCols, maxRows int, maxBytes int) (*animFrames, int, int, error) {
	cfg, err := gif.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	n := len(g.Image)
	if n < 2 {
		return nil, 0, 0, errNotAnimated
	}
	w, h := cfg.Width, cfg.Height
	if w <= 0 || h <= 0 {
		b := g.Image[0].Bounds()
		w, h = b.Dx(), b.Dy()
	}
	if w <= 0 || h <= 0 || n > maxAnimFrames || w*h*n > maxAnimPixels {
		return nil, 0, 0, errors.New("animation too large")
	}
	cols, rows := fitCells(w, h, cw, ch, maxCols, maxRows)
	dw, dh := cols*cw, rows*ch
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	out := &animFrames{}
	total := 0
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	for i, frame := range g.Image {
		var saved *image.RGBA
		disposal := byte(0)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			saved = image.NewRGBA(canvas.Bounds())
			copy(saved.Pix, canvas.Pix)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)

		dst := image.NewRGBA(image.Rect(0, 0, max(dw, 1), max(dh, 1)))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), canvas, canvas.Bounds(), draw.Src, nil)
		var buf bytes.Buffer
		if err := enc.Encode(&buf, dst); err != nil {
			return nil, 0, 0, err
		}
		if total += buf.Len(); total > maxBytes {
			return nil, 0, 0, errors.New("animation too large")
		}
		out.frames = append(out.frames, buf.Bytes())
		// Like browsers: no delay, or a tiny one, means 100ms.
		d := 100 * time.Millisecond
		if i < len(g.Delay) && g.Delay[i] >= 2 {
			d = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}
		out.delays = append(out.delays, d)

		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			canvas = saved
		}
	}
	return out, cols, rows, nil
}

// startAnims starts the tick, once, when an animated image is ready.
func (m *Model) startAnims() tea.Cmd {
	if m.animating {
		return nil
	}
	m.animating = true
	return tea.Tick(20*time.Millisecond, func(time.Time) tea.Msg { return animTick{} })
}

type framesUploadedMsg struct {
	st  *imgState
	err error
}

// uploadFrames gives frames 2… their own ids and sends them; frame 1 is
// already showing under st.id.
func (m *Model) uploadFrames(st *imgState) tea.Cmd {
	a := st.anim
	st.ids = []uint32{st.id}
	for range a.frames[1:] {
		st.ids = append(st.ids, m.newImageID())
	}
	ids, gfx, cols, rows := st.ids, m.gfx, st.cols, st.rows
	return func() tea.Msg {
		for i, id := range ids[1:] {
			if err := gfx.Show(id, a.frames[i+1], cols, rows); err != nil {
				return framesUploadedMsg{st, err}
			}
		}
		return framesUploadedMsg{st: st}
	}
}

// stepAnims moves each animation that was in the last view on to its next
// frame when due, and books the next tick. With nothing animated left, the
// tick stops.
func (m *Model) stepAnims(now time.Time) tea.Cmd {
	next, any := time.Second/2, false
	var uploads []tea.Cmd
	for _, st := range m.images {
		a := st.anim
		if a == nil || st.status != imgReady {
			continue
		}
		any = true
		if st.seen != m.viewSeq {
			continue // not on screen: stays where it was
		}
		if st.ids == nil {
			// First time on screen: the other frames go to the terminal.
			uploads = append(uploads, m.uploadFrames(st))
			continue
		}
		if !st.animReady {
			continue
		}
		if st.next.IsZero() {
			st.next = now.Add(a.delays[st.frame])
		}
		if !now.Before(st.next) {
			st.frame = (st.frame + 1) % len(a.frames)
			st.next = now.Add(a.delays[st.frame])
		}
		next = min(next, st.next.Sub(now))
	}
	if v := m.viewer; v != nil {
		if st := v.cache[v.items[v.idx].file.ID]; st != nil && st.anim != nil {
			any = true
			if st.ready && v.shown == v.items[v.idx].file.ID {
				if st.next.IsZero() {
					st.next = now.Add(st.anim.delays[st.frame])
				}
				if !now.Before(st.next) {
					st.frame = (st.frame + 1) % len(st.anim.frames)
					st.next = now.Add(st.anim.delays[st.frame])
				}
				next = min(next, st.next.Sub(now))
			}
		}
	}
	if !any {
		m.animating = false
		return nil
	}
	tick := tea.Tick(max(next, 20*time.Millisecond), func(time.Time) tea.Msg { return animTick{} })
	return tea.Batch(append(uploads, tick)...)
}
