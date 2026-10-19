package backend

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/slack-go/slack"
)

// Lists. Slack's message text (mrkdwn) has no lists: "- a" arrives as a
// hyphen. The Slack app sends what its editor builds instead, a rich_text
// block, so a message with list lines is sent the same way. The text stays
// as the fallback that notifications show.

var (
	reListItem = regexp.MustCompile(`^( *)(?:([-*•])|(\d{1,3})[.)]) +(.*)$`)
	reEmojiAt  = regexp.MustCompile(`^:([a-z0-9_+'-]*[a-z][a-z0-9_+'-]*):`)
)

// listBlocks returns the message as a rich_text block when it has list
// lines, else nil. text is Slack message text: entities like <@U1> and
// &amp; escapes, as render.Encode makes it.
func listBlocks(text string) []slack.Block {
	lines := strings.Split(text, "\n")
	hasList := false
	for _, l := range lines {
		if reListItem.MatchString(l) {
			hasList = true
			break
		}
	}
	if !hasList {
		return nil
	}

	var out []slack.RichTextElement
	var plain, quote []string
	flushPlain := func() {
		if len(plain) > 0 {
			out = append(out, slack.NewRichTextSection(inline(strings.Join(plain, "\n"), nil)...))
			plain = nil
		}
	}
	flushQuote := func() {
		if len(quote) > 0 {
			out = append(out, &slack.RichTextQuote{Type: slack.RTEQuote, Elements: inline(strings.Join(quote, "\n"), nil)})
			quote = nil
		}
	}
	var list *slack.RichTextList
	// Ordered lists continue their numbering after a nested list.
	numbered := map[int]int{}
	endList := func() { list = nil }

	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			flushPlain()
			flushQuote()
			endList()
			clear(numbered)
			body := []string{strings.TrimPrefix(strings.TrimSpace(l), "```")}
			for i++; i < len(lines); i++ {
				if before, ok := strings.CutSuffix(strings.TrimSpace(lines[i]), "```"); ok {
					body = append(body, before)
					break
				}
				body = append(body, lines[i])
			}
			code := strings.Trim(strings.Join(body, "\n"), "\n")
			out = append(out, &slack.RichTextPreformatted{Type: slack.RTEPreformatted,
				Elements: []slack.RichTextSectionElement{slack.NewRichTextSectionTextElement(unescape(code), nil)}})
			continue
		}
		if g := reListItem.FindStringSubmatch(l); g != nil {
			flushPlain()
			flushQuote()
			indent := min(len(g[1])/2, 8)
			style := slack.RTEListBullet
			if g[3] != "" {
				style = slack.RTEListOrdered
			}
			if list == nil || list.Indent != indent || list.Style != style {
				list = slack.NewRichTextList(style, indent)
				if style == slack.RTEListOrdered {
					list.Offset = numbered[indent]
				}
				for k := range numbered {
					if k > indent {
						delete(numbered, k) // a deeper list ended
					}
				}
				out = append(out, list)
			}
			list.Elements = append(list.Elements, slack.NewRichTextSection(inline(g[4], nil)...))
			if style == slack.RTEListOrdered {
				numbered[indent]++
			}
			continue
		}
		endList()
		clear(numbered)
		if q, ok := strings.CutPrefix(l, "&gt; "); ok {
			flushPlain()
			quote = append(quote, q)
			continue
		}
		flushQuote()
		plain = append(plain, l)
	}
	flushPlain()
	flushQuote()
	// A blank line kept at either end of a plain part would show as one.
	for _, e := range out {
		if s, ok := e.(*slack.RichTextSection); ok {
			trimSection(s)
		}
	}
	return []slack.Block{slack.NewRichTextBlock("", out...)}
}

// listFallback is the text for notifications and clients that can't show
// blocks: bullets as "•", numbers kept.
func listFallback(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if g := reListItem.FindStringSubmatch(l); g != nil && g[2] != "" {
			lines[i] = g[1] + "• " + g[4]
		}
	}
	return strings.Join(lines, "\n")
}

func trimSection(s *slack.RichTextSection) {
	if len(s.Elements) == 0 {
		return
	}
	if t, ok := s.Elements[0].(*slack.RichTextSectionTextElement); ok {
		t.Text = strings.TrimLeft(t.Text, "\n")
	}
	if t, ok := s.Elements[len(s.Elements)-1].(*slack.RichTextSectionTextElement); ok {
		t.Text = strings.TrimRight(t.Text, "\n")
	}
}

// inline turns one stretch of message text into rich text elements:
// `code`, *bold*, _italic_, ~strike~, <entities> and :emoji:.
func inline(s string, st *slack.RichTextSectionTextStyle) []slack.RichTextSectionElement {
	var out []slack.RichTextSectionElement
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, slack.NewRichTextSectionTextElement(unescape(buf.String()), st))
			buf.Reset()
		}
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '`':
			if j := strings.IndexByte(s[i+1:], '`'); j > 0 {
				flush()
				code := withStyle(st, func(x *slack.RichTextSectionTextStyle) { x.Code = true })
				out = append(out, slack.NewRichTextSectionTextElement(unescape(s[i+1:i+1+j]), code))
				i += j + 2
				continue
			}
		case c == '<':
			if j := strings.IndexByte(s[i:], '>'); j > 0 {
				if e := entity(s[i+1:i+j], st); e != nil {
					flush()
					out = append(out, e)
					i += j + 1
					continue
				}
			}
		case c == '*' || c == '_' || c == '~':
			if j := closing(s, i); j > 0 {
				flush()
				inner := withStyle(st, func(x *slack.RichTextSectionTextStyle) {
					switch c {
					case '*':
						x.Bold = true
					case '_':
						x.Italic = true
					case '~':
						x.Strike = true
					}
				})
				out = append(out, inline(s[i+1:j], inner)...)
				i = j + 1
				continue
			}
		case c == ':':
			if m := reEmojiAt.FindStringSubmatch(s[i:]); m != nil && (i == 0 || !isWordByte(s[i-1])) {
				flush()
				out = append(out, slack.NewRichTextSectionEmojiElement(m[1], 0, st))
				i += len(m[0])
				continue
			}
		}
		buf.WriteByte(c)
		i++
	}
	flush()
	return out
}

// closing finds the marker that ends the one opening at s[i], Slack's way:
// it opens a word and closes one, on the same line.
func closing(s string, i int) int {
	c := s[i]
	if i > 0 && (isWordByte(s[i-1]) || s[i-1] == c) {
		return -1
	}
	if i+1 >= len(s) || s[i+1] == ' ' || s[i+1] == c || s[i+1] == '\n' {
		return -1
	}
	for k := i + 2; k < len(s); k++ {
		if s[k] == '\n' {
			return -1
		}
		if s[k] == c && s[k-1] != ' ' && (k+1 == len(s) || !isWordByte(s[k+1])) {
			return k
		}
	}
	return -1
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
}

func withStyle(st *slack.RichTextSectionTextStyle, set func(*slack.RichTextSectionTextStyle)) *slack.RichTextSectionTextStyle {
	var x slack.RichTextSectionTextStyle
	if st != nil {
		x = *st
	}
	set(&x)
	return &x
}

// entity is <@U1>, <#C1|name>, <!here>, <!subteam^S1>, <https://x|label>;
// nil for anything else, which stays text.
func entity(inner string, st *slack.RichTextSectionTextStyle) slack.RichTextSectionElement {
	ref, label, _ := strings.Cut(inner, "|")
	switch {
	case strings.HasPrefix(ref, "@U") || strings.HasPrefix(ref, "@W"):
		return slack.NewRichTextSectionUserElement(ref[1:], st)
	case strings.HasPrefix(ref, "#C") || strings.HasPrefix(ref, "#G"):
		return slack.NewRichTextSectionChannelElement(ref[1:], st)
	case ref == "!here" || ref == "!channel" || ref == "!everyone":
		return &slack.RichTextSectionBroadcastElement{Type: slack.RTSEBroadcast, Range: ref[1:]}
	case strings.HasPrefix(ref, "!subteam^"):
		return slack.NewRichTextSectionUserGroupElement(ref[len("!subteam^"):])
	case strings.Contains(ref, "://") || strings.HasPrefix(ref, "mailto:"):
		return slack.NewRichTextSectionLinkElement(unescape(ref), unescape(label), st)
	}
	return nil
}

func unescape(s string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

// blocksText is the text of a message whose rich_text has a list, written
// back as message text with the list drawn as "•" and "1." lines. Slack
// flattens such a message's own text onto one line. ok is false for
// messages without a list, whose text is fine as it is.
func blocksText(blocks slack.Blocks) (text string, ok bool) {
	var rt []*slack.RichTextBlock
	for _, b := range blocks.BlockSet {
		if r, isRT := b.(*slack.RichTextBlock); isRT {
			rt = append(rt, r)
			for _, e := range r.Elements {
				if _, isList := e.(*slack.RichTextList); isList {
					ok = true
				}
			}
		}
	}
	if !ok {
		return "", false
	}
	var parts []string
	for _, r := range rt {
		for _, e := range r.Elements {
			switch e := e.(type) {
			case *slack.RichTextSection:
				parts = append(parts, strings.TrimRight(sectionText(e.Elements), "\n"))
			case *slack.RichTextList:
				for i, item := range e.Elements {
					s, isSection := item.(*slack.RichTextSection)
					if !isSection {
						continue
					}
					mark := "• "
					if e.Style == slack.RTEListOrdered {
						mark = strconv.Itoa(e.Offset+i+1) + ". "
					}
					parts = append(parts, strings.Repeat("  ", e.Indent)+mark+strings.TrimRight(sectionText(s.Elements), "\n"))
				}
			case *slack.RichTextQuote:
				for _, l := range strings.Split(strings.TrimRight(sectionText(e.Elements), "\n"), "\n") {
					parts = append(parts, "&gt; "+l)
				}
			case *slack.RichTextPreformatted:
				parts = append(parts, "```"+strings.Trim(sectionText(e.Elements), "\n")+"```")
			}
		}
	}
	return strings.Join(parts, "\n"), true
}

// sectionText writes rich text elements as message text.
func sectionText(elems []slack.RichTextSectionElement) string {
	var b strings.Builder
	for _, el := range elems {
		switch el := el.(type) {
		case *slack.RichTextSectionTextElement:
			b.WriteString(styled(escape(el.Text), el.Style))
		case *slack.RichTextSectionUserElement:
			b.WriteString("<@" + el.UserID + ">")
		case *slack.RichTextSectionChannelElement:
			b.WriteString("<#" + el.ChannelID + ">")
		case *slack.RichTextSectionUserGroupElement:
			b.WriteString("<!subteam^" + el.UsergroupID + ">")
		case *slack.RichTextSectionBroadcastElement:
			b.WriteString("<!" + el.Range + ">")
		case *slack.RichTextSectionEmojiElement:
			b.WriteString(":" + el.Name + ":")
		case *slack.RichTextSectionLinkElement:
			if el.Text != "" {
				b.WriteString(styled("<"+el.URL+"|"+escape(el.Text)+">", el.Style))
			} else {
				b.WriteString(styled("<"+el.URL+">", el.Style))
			}
		}
	}
	return b.String()
}

// styled wraps text in mrkdwn markers, keeping spaces at its ends outside
// them ("*bold* " not "*bold *", which wouldn't close).
func styled(s string, st *slack.RichTextSectionTextStyle) string {
	if st == nil || strings.TrimSpace(s) == "" {
		return s
	}
	core := strings.TrimSpace(s)
	lead := s[:strings.Index(s, core)]
	trail := s[len(lead)+len(core):]
	if st.Code {
		core = "`" + core + "`"
	}
	if st.Strike {
		core = "~" + core + "~"
	}
	if st.Italic {
		core = "_" + core + "_"
	}
	if st.Bold {
		core = "*" + core + "*"
	}
	return lead + core + trail
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
