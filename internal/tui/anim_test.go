package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
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
	if img, _ := png.Decode(bytes.NewReader(a.frames[0])); img.Bounds().Dx() != cols*8 || img.Bounds().Dy() != rows*16 {
		t.Errorf("frames should be exactly the box (%dx%d px), not %v: scaling shows seams", cols*8, rows*16, img.Bounds())
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

func TestViewerAnim(t *testing.T) {
	m := chanWithMessages(t)
	tty, _ := os.CreateTemp(t.TempDir(), "tty")
	m.SetGraphics(kitty.NewWriter(tty, false), "")
	m.cellW, m.cellH = 8, 16
	dir := filepath.Join(cacheDir(), "files")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "G1-party.gif"), testGIF(t, 3, 0), 0o600)
	gifFile := backend.File{ID: "G1", Name: "party.gif", Mimetype: "image/gif", URL: "https://files.slack.com/G1", Anim: "https://files.slack.com/G1"}
	m.update(historyMsg{id: "C2", msgs: []backend.Message{{TS: "100.000001", User: "U1", Files: []backend.File{gifFile}}}})
	m.focus = fMessages

	_, cmd := m.Update(key("v"))
	run(m, cmd) // loads it and shows the first frame
	v := m.viewer
	st := v.cache["G1"]
	if st == nil || st.anim == nil || len(st.ids) != 3 || st.ready {
		t.Fatalf("the viewer should load the GIF's frames: %+v", st)
	}
	if v.shownID(st) != st.ids[0] {
		t.Error("the first frame shows while the rest are sent")
	}
	st.ids, v.shown = nil, "" // show it again, keeping the command that sends the rest
	run(m, m.showViewer())
	if !st.ready {
		t.Fatal("frames not marked sent")
	}
	if st.cols != 5 {
		t.Errorf("a GIF plays at its own size (40px = 5 cols), not stretched: %d cols", st.cols)
	}
	now := time.Now()
	m.stepAnims(now)
	m.stepAnims(now.Add(60 * time.Millisecond))
	if st.frame != 1 || v.shownID(st) != st.ids[1] {
		t.Errorf("frame %d", st.frame)
	}
	ids := st.ids
	m.Update(key("esc"))
	if m.viewer != nil || st.ids != nil || len(ids) != 3 {
		t.Error("closing should free the frames")
	}
}

func TestAnimatedEmoji(t *testing.T) {
	// 40×20 source into a 2×1 box of 8×16 cells: 16×16 px, fitted at 16×8
	// and centred, the rest left clear.
	a, err := decodeEmojiAnim(testGIF(t, 3, gif.DisposalNone), 8, 16)
	if err != nil || len(a.frames) != 3 {
		t.Fatalf("%v, %d frames", err, len(a.frames))
	}
	img, _ := png.Decode(bytes.NewReader(a.frames[0]))
	if img.Bounds() != image.Rect(0, 0, 16, 16) {
		t.Fatalf("frames should be exactly the 2×1 box, got %v", img.Bounds())
	}
	if _, _, _, alpha := img.At(2, 1).RGBA(); alpha != 0 {
		t.Error("above the picture should stay clear")
	}
	if _, _, _, alpha := img.At(2, 5).RGBA(); alpha == 0 {
		t.Error("the picture should be centred")
	}
	if _, err := decodeEmojiAnim(testGIF(t, 1, 0), 8, 16); err != errNotAnimated {
		t.Errorf("one frame: %v", err)
	}

	m := fakeModel(t, 100, 24)
	tty, _ := os.CreateTemp(t.TempDir(), "tty")
	m.SetGraphics(kitty.NewWriter(tty, false), "")
	st := &imgState{id: 9}
	m.emojiImgs["partyparrot"] = st
	m.Update(emojiImgMsg{name: "partyparrot", png: a.frames[0], anim: a})
	if st.anim == nil || !m.animating || st.cols != 2 || st.rows != 1 {
		t.Fatalf("an animated emoji should start the tick: %+v", st)
	}
	m.viewSeq = 3
	if _, ok := m.EmojiImage("partyparrot"); !ok || st.seen != 3 {
		t.Fatal("drawing the emoji should count it as on screen")
	}
	m.stepAnims(time.Now())
	if len(st.ids) != 3 {
		t.Fatalf("on screen: its frames should go up, ids %v", st.ids)
	}
	m.Update(framesUploadedMsg{st: st})
	now := time.Now()
	m.stepAnims(now)
	m.EmojiImage("partyparrot")
	m.stepAnims(now.Add(60 * time.Millisecond))
	if got, _ := m.EmojiImage("partyparrot"); got != kitty.Inline(st.ids[1], 2) {
		t.Error("the emoji should show its second frame after 60ms")
	}
	if !isGIF("https://emoji.slack-edge.com/T1/party/abc.GIF?x=1") || isGIF("https://emoji.slack-edge.com/T1/ok/abc.png") {
		t.Error("isGIF")
	}
}
