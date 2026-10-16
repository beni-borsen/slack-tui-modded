package kitty

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// A screen that redraws every millisecond while images are sent, like
// slack-tui's startup. RIG=old sends images on a separate /dev/tty handle
// (the old way); RIG=new shares one locked writer with the TUI.
type busy struct{ n int }

type tick struct{}

func (b *busy) Init() tea.Cmd {
	return tea.Tick(time.Millisecond, func(time.Time) tea.Msg { return tick{} })
}
func (b *busy) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tick); ok {
		b.n++
		return b, b.Init()
	}
	return b, nil
}
func (b *busy) View() string {
	var s strings.Builder
	fmt.Fprintf(&s, "checking unreads %d/75 %s\n", b.n, strings.Repeat("#", b.n%40))
	for r := 0; r < 4; r++ {
		for c := 0; c < 6; c++ {
			s.WriteString(Inline(uint32(0x5A0001+r*6+c), 2) + " ")
		}
		fmt.Fprintf(&s, " row %d %d\n", r, b.n)
	}
	return s.String()
}

func noisePNG(seed int) []byte {
	rng := rand.New(rand.NewSource(int64(seed)))
	img := image.NewRGBA(image.Rect(0, 0, 96, 96))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Intn(256))
	}
	for x := 0; x < 96; x++ {
		for y := 0; y < 96; y++ {
			c := img.RGBAAt(x, y)
			c.A = 255
			img.SetRGBA(x, y, color.RGBA{c.R, c.G, c.B, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func TestRigStress(t *testing.T) {
	mode := os.Getenv("RIG")
	if mode == "" {
		t.Skip("rig only")
	}
	tty, _ := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	out := NewSyncFile(tty)
	var w *Writer
	if mode == "old" {
		sep, _ := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		w = NewWriter(sep, false)
	} else {
		w = NewWriter(out, false)
	}
	p := tea.NewProgram(&busy{}, tea.WithAltScreen(), tea.WithInput(tty), tea.WithOutput(out))
	go func() {
		time.Sleep(300 * time.Millisecond)
		for i := 0; i < 24; i++ {
			go w.Show(uint32(0x5A0001+i), noisePNG(i), 2, 1)
		}
		time.Sleep(15 * time.Second)
		p.Quit()
	}()
	p.Run()
}

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
