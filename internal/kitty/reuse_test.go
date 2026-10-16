package kitty

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestRigReuse: run 1 leaves an emoji-sized image (2x1) under an id on the
// alternate screen; run 2 reuses the id for a 24x6 picture. RIG=reuse-old
// sends run 2 the old way (no clear first), RIG=reuse-new uses Show.
func TestRigReuse(t *testing.T) {
	mode := os.Getenv("RIG")
	if !strings.HasPrefix(mode, "reuse") {
		t.Skip("rig only")
	}
	tty, _ := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	w := NewWriter(tty, false)
	const id = 0x5A0001
	enter, leave := "\x1b[?1049h\x1b[H\x1b[2J", "\x1b[?1049l"

	// Run 1: a small image, never deleted, then leave the alternate screen.
	tty.WriteString(enter)
	w.Show(id, noisePNG(1), 2, 1)
	tty.WriteString(leave)

	// Run 2: same id, big box.
	tty.WriteString(enter)
	big := noisePNG(2)
	if mode == "reuse-old" {
		tty.WriteString(w.command(fmt.Sprintf("a=T,U=1,f=100,t=d,i=%d,c=24,r=6,q=2,m=0", id), base64Std(big)))
	} else {
		w.Show(id, big, 24, 6)
	}
	tty.WriteString("run 2: " + mode + "\n")
	for _, l := range Placeholder(id, 24, 6) {
		tty.WriteString(l + "\n")
	}
	time.Sleep(20 * time.Second)
}
