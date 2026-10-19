package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/magnusdalkvist/slack-tui/internal/kitty"
)

func testGIF(t *testing.T, frames int, disposal byte) []byte {
	t.Helper()
	g := &gif.GIF{Config: image.Config{Width: 40, Height: 20, ColorModel: color.Palette(palette.Plan9)}}
	for i := range frames {
		// A different colour block per frame, each in its own corner.
		x0 := 20 * (i % 2)
		img := image.NewPaletted(image.Rect(x0, 0, x0+20, 10), palette.Plan9)
		c := color.RGBA{uint8(80 * i), 200, 50, 255}
		for y := 0; y < 10; y++ {
			for x := x0; x < x0+20; x++ {
				img.Set(x, y, c)
			}
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 5*(i+1)) // 50ms, 100ms, …
		g.Disposal = append(g.Disposal, disposal)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestScaleAnim(t *testing.T) {
	a, cols, rows, err := scaleAnim(testGIF(t, 3, gif.DisposalNone), 8, 16, 10, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.frames) != 3 || cols != 5 || rows < 1 {
		t.Fatalf("%d frames, %dx%d", len(a.frames), cols, rows)
	}
	if a.delays[0] != 50*time.Millisecond || a.delays[2] != 150*time.Millisecond {
		t.Errorf("delays %v", a.delays)
	}
	// Without disposal frame 2 still shows frame 1's corner; with
	// DisposalBackground it doesn't.
	topLeft := func(p []byte) color.Color {
		img, _ := png.Decode(bytes.NewReader(p))
		return img.At(2, 2)
	}
	if _, _, _, alpha := topLeft(a.frames[1]).RGBA(); alpha == 0 {
		t.Error("frames should build on each other")
	}
	b, _, _, _ := scaleAnim(testGIF(t, 3, gif.DisposalBackground), 8, 16, 10, 5)
	if _, _, _, alpha := topLeft(b.frames[1]).RGBA(); alpha != 0 {
		t.Error("DisposalBackground should clear the frame before")
	}
	if _, _, _, err := scaleAnim(testGIF(t, 1, 0), 8, 16, 10, 5); err != errNotAnimated {
		t.Errorf("one frame: %v", err)
	}
}

func TestStepAnims(t *testing.T) {
	m := fakeModel(t, 100, 24)
	tty, _ := os.CreateTemp(t.TempDir(), "tty")
	m.SetGraphics(kitty.NewWriter(tty, false), "")
	a, cols, rows, _ := scaleAnim(testGIF(t, 3, 0), 8, 16, 10, 5)
	st := &imgState{id: 7, status: imgReady, cols: cols, rows: rows, png: a.frames[0]}
	m.images["F1"] = st
	m.viewSeq = 5 // views have been drawn, without this image
	m.Update(imageMsg{fileID: "F1", cols: cols, rows: rows, png: a.frames[0], anim: a})
	if !m.animating || st.ids != nil {
		t.Fatal("the tick should run, and nothing more go up while off screen")
	}
	now := time.Now()
	m.stepAnims(now)
	if st.ids != nil {
		t.Fatal("frames uploaded for an image not on screen")
	}
	st.seen = m.viewSeq // on screen
	m.stepAnims(now)
	if len(st.ids) != 3 || st.ids[0] != 7 || st.animReady || st.shownID() != 7 {
		t.Fatalf("on screen: ids %v ready %v", st.ids, st.animReady)
	}
	m.Update(framesUploadedMsg{st: st})
	if !st.animReady {
		t.Fatal("uploaded: the animation should run")
	}
	m.stepAnims(now)
	m.stepAnims(now.Add(60 * time.Millisecond))
	if st.frame != 1 || st.shownID() != st.ids[1] {
		t.Errorf("frame %d after 60ms", st.frame)
	}
	m.stepAnims(now.Add(80 * time.Millisecond))
	if st.frame != 1 {
		t.Error("frame 2 isn't due yet")
	}
	// Off screen: no more frames.
	m.viewSeq++
	m.stepAnims(now.Add(time.Second))
	if st.frame != 1 {
		t.Error("an image not on screen should not animate")
	}
	delete(m.images, "F1")
	if m.stepAnims(now) != nil || m.animating {
		t.Error("the tick should stop with nothing to animate")
	}
}
