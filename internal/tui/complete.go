package tui

import (
	"regexp"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// completion is the list that opens above the input while typing an
// emoji (":ta"), a person ("@an") or a channel ("#de").
type completion struct {
	trigger string // ":", "@" or "#"
	query   string // what follows the trigger
	items   []compItem
	sel     int
}

// compItem is one row: what gets typed in, and the Slack entity it stands for.
type compItem struct {
	insert string // text put in the input, e.g. "@Anna Jensen"
	token  string // Slack form, e.g. "<@U123>"; empty for emoji
	emoji  string // shortcode, for emoji rows
	label  string // for people and channels
	detail string // dim text after the label
}

const completeRows = 8

// Like Slack: the trigger starts a word. Emoji need two shortcode
// characters, so "12:30" and a lone ":" don't open the list; people and
// channels need one.
var (
	reEmojiQuery = regexp.MustCompile(`(?:^|\s):([a-z0-9_+'\-]{2,})$`)
	reNameQuery  = regexp.MustCompile(`(?:^|\s)([@#])([\p{L}\p{N}._\-]+)$`)
)

// beforeCursor is the input text on the cursor's line, up to the cursor.
func (m *Model) beforeCursor() string {
	lines := strings.Split(m.input.Value(), "\n")
	row := m.input.Line()
	if row >= len(lines) {
		return ""
	}
	info := m.input.LineInfo()
	col := info.StartColumn + info.CharOffset
	r := []rune(lines[row])
	return string(r[:min(col, len(r))])
}

// updateCompletion opens, refreshes or closes the list after an edit.
func (m *Model) updateCompletion() {
	before := m.beforeCursor()
	trigger, query := "", ""
	if g := reEmojiQuery.FindStringSubmatch(strings.ToLower(before)); g != nil {
		trigger, query = ":", g[1]
	} else if g := reNameQuery.FindStringSubmatch(before); g != nil {
		trigger, query = g[1], g[2]
	}
	key := trigger + query
	if trigger == "" || key == m.dismissed {
		m.complete = nil
		if trigger == "" {
			m.dismissed = ""
		}
		return
	}
	if c := m.complete; c != nil && c.trigger == trigger && c.query == query {
		return
	}
	var items []compItem
	switch trigger {
	case ":":
		for _, n := range m.matchEmoji(query) {
			items = append(items, compItem{insert: ":" + n + ":", emoji: n})
		}
	case "@":
		items = m.matchPeople(query)
	case "#":
		items = m.matchChannels(query)
	}
	if len(items) == 0 {
		m.complete = nil
		return
	}
	m.complete = &completion{trigger: trigger, query: query, items: items}
}

// rank orders candidates for a query: whole name starts with it, then a
// word in it starts with it, then it appears anywhere; shorter names first
// within each group. names[i] are the strings candidate i may match on.
func rank(q string, n int, names func(i int) []string, length func(i int) int) []int {
	q = strings.ToLower(q)
	type hit struct{ i, tier, length int }
	var hits []hit
	for i := range n {
		tier := 0
		for _, s := range names(i) {
			s = strings.ToLower(s)
			switch {
			case strings.HasPrefix(s, q):
				tier = max(tier, 3)
			case strings.Contains(" "+strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(s), " "+q):
				tier = max(tier, 2)
			case strings.Contains(s, q):
				tier = max(tier, 1)
			}
		}
		if tier > 0 {
			hits = append(hits, hit{i, tier, length(i)})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int {
		if a.tier != b.tier {
			return b.tier - a.tier
		}
		return a.length - b.length
	})
	out := make([]int, len(hits))
	for k, h := range hits {
		out[k] = h.i
	}
	return out
}

// matchEmoji ranks shortcodes; when few names contain the query, fuzzy
// matches fill the list.
func (m *Model) matchEmoji(q string) []string {
	names := m.emojiNames()
	var out []string
	seen := map[string]bool{}
	for _, i := range rank(q, len(names), func(i int) []string { return names[i : i+1] }, func(i int) int { return len(names[i]) }) {
		out = append(out, names[i])
		seen[names[i]] = true
	}
	if len(out) < completeRows {
		for _, i := range fuzzyRank(q, names) {
			if !seen[names[i]] {
				out = append(out, names[i])
			}
			if len(out) >= completeRows*4 {
				break
			}
		}
	}
	return out[:min(len(out), completeRows*4)]
}

// emojiNames is every standard and custom shortcode, rebuilt when the
// custom emoji list changes.
func (m *Model) emojiNames() []string {
	if m.allEmoji == nil || m.allEmojiCustom != len(m.emoji) {
		names := render.EmojiNames()
		for n := range m.emoji {
			names = append(names, n)
		}
		slices.Sort(names)
		m.allEmoji = slices.Compact(names)
		m.allEmojiCustom = len(m.emoji)
	}
	return m.allEmoji
}

var broadcasts = []compItem{
	{insert: "@here", token: "<!here>", label: "@here", detail: "notify everyone online in this conversation"},
	{insert: "@channel", token: "<!channel>", label: "@channel", detail: "notify everyone in this conversation"},
	{insert: "@everyone", token: "<!everyone>", label: "@everyone", detail: "notify the whole workspace"},
}

// matchPeople ranks workspace members (not deactivated ones), plus the
// @here/@channel/@everyone broadcasts.
func (m *Model) matchPeople(q string) []compItem {
	var users []backend.User
	for _, u := range m.users {
		if !u.Deleted {
			users = append(users, u)
		}
	}
	slices.SortFunc(users, func(a, b backend.User) int { return strings.Compare(a.Label(), b.Label()) })
	var out []compItem
	for _, i := range rank(q, len(users),
		func(i int) []string { return []string{users[i].DisplayName, users[i].RealName, users[i].Name} },
		func(i int) int { return len(users[i].Label()) }) {
		u := users[i]
		detail := "@" + u.Name
		if u.RealName != "" && u.RealName != u.Label() {
			detail = u.RealName + " · " + detail
		}
		if u.ID == m.client.SelfID {
			detail += " · you"
		}
		out = append(out, compItem{insert: "@" + u.Label(), token: "<@" + u.ID + ">", label: "@" + u.Label(), detail: detail})
	}
	for _, i := range rank(q, len(broadcasts), func(i int) []string { return []string{broadcasts[i].insert[1:]} }, func(int) int { return 0 }) {
		out = append(out, broadcasts[i])
	}
	return out[:min(len(out), completeRows*4)]
}

// matchChannels ranks the channels we are in.
func (m *Model) matchChannels(q string) []compItem {
	var chans []*backend.Conversation
	for _, c := range m.convs {
		if !c.IsDM() {
			chans = append(chans, c)
		}
	}
	slices.SortFunc(chans, func(a, b *backend.Conversation) int { return strings.Compare(a.Name, b.Name) })
	var out []compItem
	for _, i := range rank(q, len(chans), func(i int) []string { return []string{chans[i].Name} }, func(i int) int { return len(chans[i].Name) }) {
		c := chans[i]
		detail := ""
		if c.Kind == backend.KindPrivate {
			detail = "private"
		}
		out = append(out, compItem{insert: "#" + c.Name, token: "<#" + c.ID + ">", label: "#" + c.Name, detail: detail})
	}
	return out[:min(len(out), completeRows*4)]
}

// completionKey handles keys while the list is open; ok=false passes the
// key on to the input.
func (m *Model) completionKey(k tea.KeyMsg) (tea.Cmd, bool) {
	c := m.complete
	switch k.String() {
	case "tab", "enter":
		m.acceptCompletion()
		return nil, true
	case "down", "ctrl+n":
		c.sel = min(c.sel+1, len(c.items)-1)
		return nil, true
	case "up", "ctrl+p":
		c.sel = max(c.sel-1, 0)
		return nil, true
	case "esc":
		m.dismissed = c.trigger + c.query // stay closed until the word changes
		m.complete = nil
		return nil, true
	}
	return nil, false
}

// acceptCompletion swaps the typed trigger and query before the cursor for
// the chosen item, and remembers its Slack form for when the text is sent.
func (m *Model) acceptCompletion() {
	c := m.complete
	it := c.items[c.sel]
	for range len([]rune(c.query)) + 1 { // the query and its trigger
		m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m.input.InsertString(it.insert + " ")
	if it.token != "" {
		tokens := m.composeTokens
		if m.editing != nil {
			tokens = m.editing.tokens
		}
		tokens[it.insert] = it.token
	}
	m.complete = nil
	m.fitInput()
}

// renderCompletion draws the list that sits above the input box.
func (m *Model) renderCompletion(w int) []string {
	return m.renderCompItems(m.complete)
}

func (m *Model) renderCompItems(c *completion) []string {
	start := max(0, c.sel-completeRows+1)
	var lines []string
	for i := start; i < len(c.items) && i < start+completeRows; i++ {
		it := c.items[i]
		var row string
		if it.emoji != "" {
			row = m.emojiChip(it.emoji) + "  " + it.insert
		} else {
			row = it.label
			if it.detail != "" {
				row += "  " + sDim.Render(it.detail)
			}
		}
		if i == c.sel {
			lines = append(lines, sBar.Render("▌")+" "+row)
		} else {
			lines = append(lines, "  "+row)
		}
	}
	lines = append(lines, sFaint.Render("  tab insert · ↑↓ choose · esc close"))
	return lines
}

// visible lists the emoji on screen, so custom ones load.
func (c *completion) visible() []string {
	var out []string
	start := max(0, c.sel-completeRows+1)
	for _, it := range c.items[start:min(len(c.items), start+completeRows)] {
		if it.emoji != "" {
			out = append(out, it.emoji)
		}
	}
	return out
}
