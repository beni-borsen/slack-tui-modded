// Package render turns Slack's mrkdwn into styled terminal text.
package render

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Resolver looks up names for the IDs Slack embeds in message text.
type Resolver interface {
	UserName(id string) string
	ChannelName(id string) string
	SelfID() string
	// CustomEmoji returns emoji.list's value for a custom emoji: an image
	// URL, or "alias:<name>".
	CustomEmoji(name string) (string, bool)
	// EmojiImage returns text that draws a custom emoji as a picture, once
	// it has loaded and the terminal can show images.
	EmojiImage(name string) (string, bool)
}

type Styles struct {
	Bold, Italic, Strike  lipgloss.Style
	Code, CodeBlock       lipgloss.Style
	Quote                 lipgloss.Style
	Link                  lipgloss.Style
	Mention, SelfMention  lipgloss.Style
	Channel, UnknownEmoji lipgloss.Style
	CustomEmoji           lipgloss.Style
}

var (
	reEntity = regexp.MustCompile(`<([^<>\n]+)>`)
	reCode   = regexp.MustCompile("`([^`\n]+)`")
	reEmoji  = regexp.MustCompile(`:([a-z0-9_+'\-]+):(?::skin-tone-\d:)?`)
	// Slack only formats when the markers sit at word boundaries.
	reBold   = regexp.MustCompile(`(^|[\s(\[{"'>\x00])\*([^*\s](?:[^*\n]*?[^*\s])?)\*($|[\s.,!?;:)\]}"'<\x00])`)
	reItalic = regexp.MustCompile(`(^|[\s(\[{"'>\x00])_([^_\s](?:[^_\n]*?[^_\s])?)_($|[\s.,!?;:)\]}"'<\x00])`)
	reStrike = regexp.MustCompile(`(^|[\s(\[{"'>\x00])~([^~\s](?:[^~\n]*?[^~\s])?)~($|[\s.,!?;:)\]}"'<\x00])`)
)

// ResolveEmoji turns a shortcode (without colons) into its character,
// following custom aliases. custom is true for image-only custom emoji,
// which a text terminal cannot draw.
func ResolveEmoji(name string, r Resolver) (char string, custom bool) {
	name = BaseEmoji(name)
	for range 5 { // aliases can chain; Slack keeps them short
		if e, ok := emojiTable[name]; ok {
			return e, false
		}
		v, ok := r.CustomEmoji(name)
		if !ok {
			return "", false
		}
		target, isAlias := strings.CutPrefix(v, "alias:")
		if !isAlias {
			return "", true
		}
		name = target
	}
	return "", false
}

// BaseEmoji drops a skin tone: reactions arrive as "pray::skin-tone-2",
// and they are shown as the plain emoji.
func BaseEmoji(name string) string {
	base, _, _ := strings.Cut(name, "::")
	return base
}

// CustomTarget follows custom aliases to the emoji that has the image.
func CustomTarget(name string, r Resolver) string {
	for range 5 {
		v, ok := r.CustomEmoji(name)
		target, isAlias := strings.CutPrefix(v, "alias:")
		if !ok || !isAlias {
			return name
		}
		name = target
	}
	return name
}

// Emoji renders a shortcode: the character, a styled :name: chip for
// custom image emoji, or dim :name: when unknown.
func Emoji(name string, r Resolver, st Styles) string {
	char, custom := ResolveEmoji(name, r)
	switch {
	case char != "":
		return char
	case custom:
		if img, ok := r.EmojiImage(CustomTarget(name, r)); ok {
			return img
		}
		return st.CustomEmoji.Render(":" + name + ":")
	default:
		return st.UnknownEmoji.Render(":" + name + ":")
	}
}

// Mrkdwn renders message text. Lines are not wrapped.
func Mrkdwn(text string, r Resolver, st Styles) string {
	text = sanitizeWidth(text)
	var out []string
	parts := strings.Split(text, "```")
	for i, part := range parts {
		if i%2 == 1 && i < len(parts)-1 {
			out = append(out, codeBlock(part, st))
			continue
		}
		if i%2 == 1 { // unclosed fence: treat literally
			part = "```" + part
		}
		out = append(out, inline(part, r, st))
	}
	return strings.Trim(strings.Join(out, ""), "\n")
}

func codeBlock(s string, st Styles) string {
	s = strings.Trim(unescape(s), "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = st.CodeBlock.Render(" " + l + " ")
	}
	return "\n" + strings.Join(lines, "\n") + "\n"
}

func inline(s string, r Resolver, st Styles) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		quote := false
		if rest, ok := strings.CutPrefix(l, "&gt; "); ok {
			l, quote = rest, true
		} else if l == "&gt;" {
			l, quote = "", true
		}
		l = inlineLine(l, r, st)
		if quote {
			l = st.Quote.Render("▎") + " " + l
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}

// inlineLine swaps code spans and <…> entities for placeholders, applies
// the emphasis rules to what is left, then puts the placeholders back. That
// keeps underscores in URLs and code from turning into italics.
func inlineLine(l string, r Resolver, st Styles) string {
	var held []string
	hold := func(s string) string {
		held = append(held, s)
		return fmt.Sprintf("\x00%d\x00", len(held)-1)
	}
	l = reCode.ReplaceAllStringFunc(l, func(m string) string {
		return hold(st.Code.Render(unescape(m[1 : len(m)-1])))
	})
	l = reEntity.ReplaceAllStringFunc(l, func(m string) string {
		return hold(entity(m[1:len(m)-1], r, st))
	})
	l = unescape(l)
	for range 2 { // second pass catches adjacent matches that shared a boundary
		l = reBold.ReplaceAllStringFunc(l, wrapWith(reBold, st.Bold))
		l = reItalic.ReplaceAllStringFunc(l, wrapWith(reItalic, st.Italic))
		l = reStrike.ReplaceAllStringFunc(l, wrapWith(reStrike, st.Strike))
	}
	l = reEmoji.ReplaceAllStringFunc(l, func(m string) string {
		return Emoji(reEmoji.FindStringSubmatch(m)[1], r, st)
	})
	for i, h := range held {
		l = strings.Replace(l, fmt.Sprintf("\x00%d\x00", i), h, 1)
	}
	return l
}

func wrapWith(re *regexp.Regexp, style lipgloss.Style) func(string) string {
	return func(m string) string {
		g := re.FindStringSubmatch(m)
		return g[1] + style.Render(g[2]) + g[3]
	}
}

// entity renders <@U123>, <#C123|name>, <!here>, <https://…|label> and friends.
func entity(e string, r Resolver, st Styles) string {
	target, label, _ := strings.Cut(e, "|")
	switch {
	case strings.HasPrefix(target, "@"):
		id := target[1:]
		name := label
		if n := r.UserName(id); n != "" {
			name = n
		}
		if name == "" {
			name = id
		}
		if id == r.SelfID() {
			return st.SelfMention.Render("@" + name)
		}
		return st.Mention.Render("@" + name)
	case strings.HasPrefix(target, "#"):
		name := label
		if name == "" {
			name = r.ChannelName(target[1:])
		}
		if name == "" {
			name = target[1:]
		}
		return st.Channel.Render("#" + name)
	case strings.HasPrefix(target, "!subteam^"):
		if label == "" {
			label = "@group"
		}
		return st.Mention.Render(label)
	case strings.HasPrefix(target, "!"):
		name := strings.TrimPrefix(target, "!")
		if label != "" {
			name = strings.TrimPrefix(label, "@")
		}
		if name == "date" || strings.HasPrefix(name, "date^") {
			return unescape(label)
		}
		return st.SelfMention.Render("@" + name)
	default:
		url := unescape(target)
		if label != "" && label != url {
			return st.Link.Render(unescape(label))
		}
		return st.Link.Render(strings.TrimPrefix(strings.TrimPrefix(url, "mailto:"), "tel:"))
	}
}

func unescape(s string) string {
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	return strings.ReplaceAll(s, "&amp;", "&")
}

// EmojiNames lists every standard shortcode, sorted.
func EmojiNames() []string {
	names := make([]string, 0, len(emojiTable))
	for n := range emojiTable {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// EmojiShortcodes finds the :shortcode: names used in message text.
func EmojiShortcodes(text string) []string {
	var out []string
	for _, m := range reEmoji.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// inTmux: tmux measures some characters differently from the terminal and
// from us, and a row it thinks is too wide wraps and shifts the screen.
var inTmux = os.Getenv("TMUX") != ""

// sanitizeWidth drops characters whose width terminals disagree on: soft
// hyphens, and under tmux the skin-tone modifiers, which tmux counts as a
// second emoji (👍🏽 as four cells, not two).
func sanitizeWidth(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\u00ad':
			return -1
		case inTmux && r >= 0x1F3FB && r <= 0x1F3FF:
			return -1
		}
		return r
	}, s)
}
