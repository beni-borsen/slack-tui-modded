package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/kitty"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// Custom emoji are drawn as small images, two cells wide like ordinary
// emoji, once they have loaded.

type emojiImgMsg struct {
	name string
	png  []byte
	err  error
}

// EmojiImage implements render.Resolver.
func (m *Model) EmojiImage(name string) (string, bool) {
	if m.gfx == nil {
		return "", false
	}
	if st := m.emojiImgs[name]; st != nil && st.status == imgReady {
		return kitty.Inline(st.id, 2), true
	}
	return "", false
}

// scanEmoji starts loading the custom emoji used by what is on screen.
func (m *Model) scanEmoji() tea.Cmd {
	if m.gfx == nil || len(m.emoji) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	want := func(name string) {
		target := render.CustomTarget(name, m)
		url, ok := m.emoji[target]
		if !ok || strings.HasPrefix(url, "alias:") || m.emojiImgs[target] != nil {
			return
		}
		m.emojiImgs[target] = &imgState{id: m.newImageID()}
		cmds = append(cmds, m.fetchEmoji(target, url, m.emojiImgs[target].id))
	}
	scanMsgs := func(msgs []backend.Message) {
		for _, msg := range msgs {
			for _, n := range render.EmojiShortcodes(msg.Text) {
				want(n)
			}
			for _, r := range msg.Reactions {
				want(r.Name)
			}
		}
	}
	if l := m.lists[m.current]; l != nil && m.current != unreadsID {
		scanMsgs(l.msgs)
	}
	if m.thread != nil {
		scanMsgs(m.thread.list.msgs)
	}
	if m.current == unreadsID {
		for _, msgs := range m.unreadMsgs {
			scanMsgs(msgs)
		}
	}
	for _, sec := range m.cfg.Sections {
		if n := strings.Trim(sec.Icon, ":"); n != "" {
			want(n)
		}
	}
	if p := m.picker; p != nil {
		for _, n := range p.visible() {
			want(n)
		}
	}
	if c := m.complete; c != nil {
		for _, n := range c.visible() {
			want(n)
		}
	}
	if c := m.cmd; c != nil && c.complete != nil {
		for _, n := range c.complete.visible() {
			want(n)
		}
	}
	// Status emoji: the user's own, and those of people in the DM list.
	wantStatus := func(userID string) {
		if e, _ := m.users[userID].Status(time.Now()); e != "" {
			want(strings.Trim(e, ":"))
		}
	}
	wantStatus(m.client.SelfID)
	for _, it := range m.side {
		if c := m.convs[it.id]; c != nil && c.Kind == backend.KindDM {
			wantStatus(c.UserID)
		}
	}
	return tea.Batch(cmds...)
}

// fetchEmoji loads one custom emoji, from the disk cache when possible.
func (m *Model) fetchEmoji(name, url string, id uint32) tea.Cmd {
	cw, ch := m.cellW, m.cellH
	return func() tea.Msg {
		m.imgSem <- struct{}{}
		defer func() { <-m.imgSem }()

		sum := sha256.Sum256([]byte(url))
		path := filepath.Join(cacheDir(), "emoji", hex.EncodeToString(sum[:12])+".png")
		pngData, err := os.ReadFile(path)
		if err != nil {
			raw, err := backend.DownloadEmoji(m.ctx, url)
			if err != nil {
				return emojiImgMsg{name: name, err: err}
			}
			if pngData, _, _, err = fitImage(raw, cw, ch, 2, 1); err != nil {
				return emojiImgMsg{name: name, err: err}
			}
			if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
				os.WriteFile(path, pngData, 0o600)
			}
		}
		if err := m.gfx.Show(id, pngData, 2, 1); err != nil {
			return emojiImgMsg{name: name, err: err}
		}
		return emojiImgMsg{name: name, png: pngData}
	}
}

// sectionIcon is a section header's emoji, or "" while a custom one loads.
func (m *Model) sectionIcon(section string) string {
	for _, sec := range m.cfg.Sections {
		if sec.Name != section || sec.Icon == "" {
			continue
		}
		name := strings.Trim(sec.Icon, ":")
		if e, _ := render.ResolveEmoji(name, m); e != "" {
			return e
		}
		if img, ok := m.EmojiImage(render.CustomTarget(name, m)); ok {
			return img
		}
	}
	// The built-in sections get Slack's own icons, as Nerd Font glyphs
	// (Octicons): a hash for channels, two speech bubbles for DMs.
	switch section {
	case "Channels":
		return sHeader.Render("\uf4df")
	case "Direct messages":
		return sHeader.Render("\uf442")
	case "External":
		return sHeader.Render("\uf42b")
	}
	return ""
}

// emojiChip renders a reaction's emoji: the character, the custom image,
// or :name: when neither is available.
func (m *Model) emojiChip(name string) string {
	if e, _ := render.ResolveEmoji(name, m); e != "" {
		return e
	}
	if img, ok := m.EmojiImage(render.CustomTarget(name, m)); ok {
		return img
	}
	return ":" + name + ":"
}
