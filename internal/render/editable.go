package render

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Editable turns message text into what a person would type: mentions and
// channel links become @Name and #channel, and &amp;/&lt;/&gt; are
// unescaped. The returned tokens let Encode turn them back.
func Editable(text string, r Resolver) (string, map[string]string) {
	tokens := map[string]string{}
	// Entities kept in raw form are set aside so unescaping skips them.
	var kept []string
	keep := func(s string) string {
		kept = append(kept, s)
		return fmt.Sprintf("\x00%d\x00", len(kept)-1)
	}
	out := reEntity.ReplaceAllStringFunc(text, func(m string) string {
		inner := m[1 : len(m)-1]
		target, label, _ := strings.Cut(inner, "|")
		var tok string
		switch {
		case strings.HasPrefix(target, "@"):
			name := r.UserName(target[1:])
			if name == "" {
				name = label
			}
			if name == "" {
				return keep(m) // unknown user: keep the raw mention so it survives
			}
			tok = "@" + name
		case strings.HasPrefix(target, "#"):
			name := label
			if name == "" {
				name = r.ChannelName(target[1:])
			}
			if name == "" {
				return keep(m)
			}
			tok = "#" + name
		case strings.HasPrefix(target, "!subteam^") && label != "":
			tok = label
		case strings.HasPrefix(target, "!"):
			tok = "@" + strings.TrimPrefix(target, "!")
		case label == "" || label == target:
			return target // a bare link; Slack links URLs by itself
		case strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://"):
			return "[" + label + "](" + target + ")" // Encode turns it back into a link
		default:
			return keep(m) // a labelled link with no typed form; keep it as is
		}
		tokens[tok] = m
		return tok
	})
	out = unescape(out)
	for i, k := range kept {
		out = strings.Replace(out, fmt.Sprintf("\x00%d\x00", i), k, 1)
	}
	return out, tokens
}

// Encode turns typed text into Slack message text: &, < and > are escaped,
// except inside raw entities, and known @mentions/#channels become entities.
func Encode(text string, tokens map[string]string) string {
	var held []string
	hold := func(s string) string {
		held = append(held, s)
		return fmt.Sprintf("\x00%d\x00", len(held)-1)
	}
	// Raw entities typed or kept from the original stay untouched.
	text = reEntity.ReplaceAllStringFunc(text, func(m string) string {
		inner := m[1 : len(m)-1]
		if strings.HasPrefix(inner, "@U") || strings.HasPrefix(inner, "@W") || strings.HasPrefix(inner, "#C") ||
			strings.HasPrefix(inner, "!") || strings.Contains(inner, "://") || strings.HasPrefix(inner, "mailto:") {
			return hold(m)
		}
		return m
	})
	// Markdown links, [label](https://…), become Slack's <url|label>.
	text = reMdLink.ReplaceAllStringFunc(text, func(m string) string {
		g := reMdLink.FindStringSubmatch(m)
		return hold("<" + escape(g[2]) + "|" + escape(g[1]) + ">")
	})
	// Longest token first, so "@Anna Jensen" wins over "@Anna".
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int { return len(b) - len(a) })
	for _, k := range keys {
		text = strings.ReplaceAll(text, k, hold(tokens[k]))
	}
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	for i := len(held) - 1; i >= 0; i-- {
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00%d\x00", i), held[i])
	}
	return text
}

var reMdLink = regexp.MustCompile(`\[([^\[\]\n|]+)\]\((https?://[^\s()<>|]+)\)`)

func escape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}
