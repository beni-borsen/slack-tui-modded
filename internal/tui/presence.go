package tui

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/config"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// Status, Do Not Disturb and presence: the user's own (set with :status,
// :dnd, :away and :back, shown in the status bar) and other people's
// (status and a green icon for active people in the DM list).

const presenceMax = 50 // people asked about per round

type meMsg struct {
	dnd  backend.DND
	away bool
	err  error
}

type dndMsg struct {
	dnd backend.DND
	err error
}

// dndTick fires when the DND state changes on its own (a snooze or the
// scheduled hours end), so the status bar and the next window catch up.
type dndTick struct{ seq int }

type presenceMsg struct {
	active map[string]bool
	poll   bool // a scheduled round, which books the next one
	err    error
}

type presenceTick struct{}

type statusSetMsg struct {
	emoji, text string
	expires     time.Time
	err         error
}

type awayMsg struct {
	away bool
	err  error
}

// loadMe fetches the user's own DND state and whether they set themselves away.
func (m *Model) loadMe() tea.Cmd {
	return func() tea.Msg {
		dnd, err := m.client.DNDInfo(m.ctx)
		if err != nil {
			return meMsg{err: err}
		}
		p, err := m.client.Presence(m.ctx, m.client.SelfID)
		return meMsg{dnd: dnd, away: p.ManualAway, err: err}
	}
}

func (m *Model) reloadDND() tea.Cmd {
	return func() tea.Msg {
		d, err := m.client.DNDInfo(m.ctx)
		return dndMsg{dnd: d, err: err}
	}
}

// setDND stores the state and books a refresh for when it next changes.
func (m *Model) setDND(d backend.DND) tea.Cmd {
	m.dnd = d
	m.dndSeq++
	next := d.Next(time.Now())
	if next.IsZero() {
		return nil
	}
	seq := m.dndSeq
	return tea.Tick(time.Until(next)+time.Second, func(time.Time) tea.Msg { return dndTick{seq} })
}

// presenceUsers are the people whose DMs are on screen in the sidebar,
// and the one in the open DM.
func (m *Model) presenceUsers() []string {
	seen := map[string]bool{}
	var ids []string
	add := func(convID string) {
		c := m.convs[convID]
		if c == nil || c.Kind != backend.KindDM || c.UserID == m.client.SelfID || seen[c.UserID] {
			return
		}
		if u := m.users[c.UserID]; u.IsBot || u.Deleted {
			return
		}
		seen[c.UserID] = true
		ids = append(ids, c.UserID)
	}
	add(m.current)
	for _, it := range m.side {
		if len(ids) >= presenceMax {
			break
		}
		add(it.id)
	}
	return ids
}

// fetchPresence asks Slack about each person, a few at a time.
func (m *Model) fetchPresence(ids []string, poll bool) tea.Cmd {
	if len(ids) == 0 {
		if poll {
			return m.presenceLater()
		}
		return nil
	}
	return func() tea.Msg {
		var (
			mu      sync.Mutex
			wg      sync.WaitGroup
			active  = map[string]bool{}
			lastErr error
		)
		jobs := make(chan string)
		for range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for id := range jobs {
					p, err := m.client.Presence(m.ctx, id)
					mu.Lock()
					if err != nil {
						lastErr = err
					} else {
						active[id] = p.Active
					}
					mu.Unlock()
				}
			}()
		}
		for _, id := range ids {
			jobs <- id
		}
		close(jobs)
		wg.Wait()
		if len(active) > 0 {
			lastErr = nil // a few failures aren't worth a message
		}
		return presenceMsg{active: active, poll: poll, err: lastErr}
	}
}

func (m *Model) pollPresence() tea.Cmd {
	if m.cfg.PresenceMinutes < 0 {
		return nil
	}
	return m.fetchPresence(m.presenceUsers(), true)
}

func (m *Model) presenceLater() tea.Cmd {
	return tea.Tick(time.Duration(m.cfg.PresenceMinutes)*time.Minute, func(time.Time) tea.Msg { return presenceTick{} })
}

// presenceFor fetches one person's presence when the open DM is someone
// the last round didn't cover.
func (m *Model) presenceFor(convID string) tea.Cmd {
	c := m.convs[convID]
	if m.cfg.PresenceMinutes < 0 || c == nil || c.Kind != backend.KindDM || c.UserID == m.client.SelfID {
		return nil
	}
	if _, ok := m.presence[c.UserID]; ok {
		return nil
	}
	return m.fetchPresence([]string{c.UserID}, false)
}

// statusEmoji is a person's status emoji as it can be drawn: the
// character, the custom image once loaded, else "".
func (m *Model) statusEmoji(userID string) string {
	emoji, _ := m.users[userID].Status(time.Now())
	name := strings.Trim(emoji, ":")
	if name == "" {
		return ""
	}
	if e, _ := render.ResolveEmoji(name, m); e != "" {
		return e
	}
	if img, ok := m.EmojiImage(render.CustomTarget(name, m)); ok {
		return img
	}
	return ""
}

// statusLine is "<emoji> text" for a person's status, "" without one.
func (m *Model) statusLine(userID string) string {
	emoji, text := m.users[userID].Status(time.Now())
	if emoji == "" && text == "" {
		return ""
	}
	var parts []string
	if name := strings.Trim(emoji, ":"); name != "" {
		parts = append(parts, render.Emoji(name, m, mdStyles))
	}
	if text != "" {
		parts = append(parts, render.Mrkdwn(text, m, mdStyles))
	}
	return strings.Join(parts, " ")
}

// clock formats a time for "until …": the time today, else with the weekday,
// else with the date.
func clock(t time.Time, layout string) string {
	now := time.Now()
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return t.Format(layout)
	case t.Sub(now) < 6*24*time.Hour:
		return t.Format("Mon " + layout)
	default:
		return t.Format("Jan 2 " + layout)
	}
}

// --- :status ----------------------------------------------------------------

var (
	reStatusEmoji = regexp.MustCompile(`^:[a-z0-9_+'\-]+:(?::skin-tone-\d:)?$`)
	reDuration    = regexp.MustCompile(`^(?:\d+[wdhm])+$`)
	reDurPart     = regexp.MustCompile(`(\d+)([wdhm])`)
	reClock       = regexp.MustCompile(`^(\d{1,2})[:.](\d{2})$`)
	// After "until": 15, 15:30, 15.30, 1530, 3pm, 3:30pm.
	reUntilClock = regexp.MustCompile(`^(\d{1,2})(?:[:.]?(\d{2}))?(am|pm)?$`)
)

// parseDuration reads "30m", "1h30m", "2d" or "1w"; a bare number is minutes.
func parseDuration(s string) (time.Duration, bool) {
	s = strings.ToLower(s)
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return time.Duration(n) * time.Minute, true
	}
	if !reDuration.MatchString(s) {
		return 0, false
	}
	unit := map[string]time.Duration{"w": 7 * 24 * time.Hour, "d": 24 * time.Hour, "h": time.Hour, "m": time.Minute}
	var d time.Duration
	for _, g := range reDurPart.FindAllStringSubmatch(s, -1) {
		n, _ := strconv.Atoi(g[1])
		d += time.Duration(n) * unit[g[2]]
	}
	return d, d > 0
}

// parseUntil reads the end of an expiry: "HH:MM" (the next time the clock
// shows it) or "tomorrow" (midnight tonight).
func parseUntil(s string, now time.Time) (time.Time, bool) {
	s = strings.ToLower(s)
	if s == "tomorrow" {
		y, mo, d := now.Date()
		return time.Date(y, mo, d+1, 0, 0, 0, 0, now.Location()), true
	}
	h, mi, bare, ok := parseClock(s)
	if !ok {
		return time.Time{}, false
	}
	y, mo, d := now.Date()
	t := time.Date(y, mo, d, h, mi, 0, 0, now.Location())
	// A bare small hour already past ("until 3" at 10:00) means the
	// afternoon; with minutes ("until 8:00") it is taken as written.
	if !t.After(now) && bare && h < 12 && t.Add(12*time.Hour).After(now) {
		t = t.Add(12 * time.Hour)
	}
	if !t.After(now) {
		t = t.AddDate(0, 0, 1)
	}
	return t, true
}

// parseClock reads 15, 15:30, 15.30, 1530, 3pm or 3:30pm. bare is a lone
// hour without minutes or am/pm, which callers may read as the afternoon.
func parseClock(s string) (h, mi int, bare, ok bool) {
	g := reUntilClock.FindStringSubmatch(strings.ToLower(s))
	if g == nil {
		return 0, 0, false, false
	}
	h, _ = strconv.Atoi(g[1])
	mi, _ = strconv.Atoi(g[2])
	switch g[3] {
	case "am", "pm":
		if h < 1 || h > 12 {
			return 0, 0, false, false
		}
		h %= 12
		if g[3] == "pm" {
			h += 12
		}
	}
	if h > 23 || mi > 59 {
		return 0, 0, false, false
	}
	return h, mi, g[2] == "" && g[3] == "", true
}

// isUntil accepts "until" and its common misspellings and short forms.
func isUntil(w string) bool {
	switch strings.ToLower(w) {
	case "until", "untill", "till", "til":
		return true
	}
	return false
}

// cutExpiry takes an expiry off the end of the words: a duration
// ("30m", "2h", "1d") or "until 17:00" / "until tomorrow".
func cutExpiry(words []string, now time.Time) ([]string, time.Time) {
	n := len(words)
	// "until 3 pm": the am/pm as its own word.
	if n >= 3 && isUntil(words[n-3]) && (strings.EqualFold(words[n-1], "am") || strings.EqualFold(words[n-1], "pm")) {
		if t, ok := parseUntil(words[n-2]+words[n-1], now); ok {
			return words[:n-3], t
		}
	}
	if n >= 2 && isUntil(words[n-2]) {
		if t, ok := parseUntil(words[n-1], now); ok {
			return words[:n-2], t
		}
	}
	if n >= 1 && !reClock.MatchString(words[n-1]) {
		if _, err := strconv.Atoi(words[n-1]); err != nil { // "back in 5" stays text
			if d, ok := parseDuration(words[n-1]); ok {
				return words[:n-1], now.Add(d)
			}
		}
	}
	return words, time.Time{}
}

// parseStatus reads ":emoji: text [30m | until 17:00 | until tomorrow]".
func parseStatus(args string, now time.Time) (emoji, text string, expires time.Time, err error) {
	words := strings.Fields(args)
	if len(words) > 0 && reStatusEmoji.MatchString(words[0]) {
		emoji, words = words[0], words[1:]
	}
	words, expires = cutExpiry(words, now)
	text = strings.Join(words, " ")
	if emoji == "" && text == "" {
		return "", "", time.Time{}, errors.New("usage: :status :emoji: text [30m | until 17:00 | until tomorrow]")
	}
	if n := len([]rune(text)); n > 100 {
		return "", "", time.Time{}, fmt.Errorf("status text is %d characters, Slack allows 100", n)
	}
	return emoji, text, expires, nil
}

func (m *Model) cmdStatus(args string) tea.Cmd {
	args = strings.TrimSpace(args)
	switch strings.ToLower(args) {
	case "":
		line := m.statusLine(m.client.SelfID)
		if line == "" {
			return m.setStatus("no status set", false)
		}
		if exp := m.users[m.client.SelfID].StatusExpires; exp > 0 {
			line += " until " + clock(time.Unix(exp, 0), m.cfg.TimeFormat)
		}
		return m.setStatus("status: "+line, false)
	case "clear", "off", "none":
		return m.sendStatus("", "", time.Time{})
	}
	emoji, text, expires, err := m.statusFromTemplate(args, time.Now())
	if errors.Is(err, errNoTemplate) {
		emoji, text, expires, err = parseStatus(args, time.Now())
	}
	if err != nil {
		return m.setStatus(err.Error(), true)
	}
	if name := strings.Trim(emoji, ":"); name != "" {
		if char, custom := render.ResolveEmoji(name, m); char == "" && !custom {
			return m.setStatus("unknown emoji "+emoji, true)
		}
	}
	return m.sendStatus(emoji, text, expires)
}

var errNoTemplate = errors.New("no such template")

// statusFromTemplate reads "<template> [expiry]": a status from the
// config, with its expiry replaced when one is given.
func (m *Model) statusFromTemplate(args string, now time.Time) (emoji, text string, expires time.Time, err error) {
	words := strings.Fields(args)
	if len(words) == 0 {
		return "", "", time.Time{}, errNoTemplate
	}
	tpl, ok := m.statusTemplate(words[0])
	if !ok {
		return "", "", time.Time{}, errNoTemplate
	}
	exp := tpl.Expires
	if len(words) > 1 {
		exp = strings.Join(words[1:], " ")
	}
	if exp != "" {
		rest, t := cutExpiry(strings.Fields(exp), now)
		if len(rest) > 0 || t.IsZero() {
			return "", "", time.Time{}, fmt.Errorf("status %s: can't read expiry %q (30m, 2h, until 17:00, until tomorrow)", tpl.Name, exp)
		}
		expires = t
	}
	if tpl.Emoji != "" {
		emoji = ":" + strings.Trim(tpl.Emoji, ":") + ":"
	}
	return emoji, tpl.Text, expires, nil
}

func (m *Model) statusTemplate(name string) (config.StatusTemplate, bool) {
	for _, t := range m.cfg.Statuses {
		if t.Name != "" && strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return config.StatusTemplate{}, false
}

// templateItems suggests status templates while the first word after
// ":status " is typed; nil when that isn't what is being typed.
func (m *Model) templateItems(text string) []compItem {
	name, arg, ok := strings.Cut(text, " ")
	if !ok || strings.Contains(arg, " ") || strings.HasPrefix(arg, ":") {
		return nil
	}
	if c, ok := findCommand(name); !ok || c.name != "status" {
		return nil
	}
	var items []compItem
	for _, t := range m.cfg.Statuses {
		if t.Name == "" || !strings.HasPrefix(strings.ToLower(t.Name), strings.ToLower(arg)) || strings.EqualFold(t.Name, arg) {
			continue
		}
		detail := t.Text
		if t.Expires != "" {
			detail = strings.TrimSpace(detail + " · " + t.Expires)
		}
		items = append(items, compItem{insert: t.Name, label: t.Name, emoji: strings.Trim(t.Emoji, ":"), detail: detail})
	}
	return items
}

func (m *Model) sendStatus(emoji, text string, expires time.Time) tea.Cmd {
	return func() tea.Msg {
		err := m.client.SetStatus(m.ctx, emoji, text, expires)
		return statusSetMsg{emoji: emoji, text: text, expires: expires, err: err}
	}
}

func (m *Model) statusSet(msg statusSetMsg) tea.Cmd {
	if msg.err != nil {
		return m.setStatus("status: "+msg.err.Error(), true)
	}
	// The user_change event says the same a moment later.
	me := m.users[m.client.SelfID]
	me.StatusEmoji, me.StatusText, me.StatusExpires = msg.emoji, msg.text, 0
	if !msg.expires.IsZero() {
		me.StatusExpires = msg.expires.Unix()
	}
	m.users[me.ID] = me
	if msg.emoji == "" && msg.text == "" {
		return m.setStatus("status cleared", false)
	}
	return m.setStatus("status set", false)
}

// --- :dnd -------------------------------------------------------------------

func (m *Model) cmdDND(args string) tea.Cmd {
	args = strings.TrimSpace(args)
	now := time.Now()
	switch strings.ToLower(args) {
	case "":
		if until, on := m.dnd.Until(now); on {
			return m.setStatus("notifications paused until "+clock(until, m.cfg.TimeFormat), false)
		}
		if !m.dnd.Start.IsZero() {
			return m.setStatus(fmt.Sprintf("notifications on; quiet hours %s–%s",
				clock(m.dnd.Start, m.cfg.TimeFormat), clock(m.dnd.End, m.cfg.TimeFormat)), false)
		}
		return m.setStatus("notifications on", false)
	case "off", "end", "stop":
		if _, on := m.dnd.Until(now); !on {
			return m.setStatus("notifications are already on", false)
		}
		d := m.dnd
		return func() tea.Msg {
			nd, err := m.client.EndDND(m.ctx, d)
			return dndMsg{dnd: nd, err: err}
		}
	}
	words, until := cutExpiry(strings.Fields(args), now)
	if len(words) > 0 {
		// cutExpiry leaves bare numbers alone for :status; here they are minutes.
		if d, ok := parseDuration(args); ok {
			until, words = now.Add(d), nil
		}
	}
	if len(words) > 0 || until.IsZero() {
		return m.setStatus("usage: :dnd 30m | 2h | until 17:00 | until tomorrow | off", true)
	}
	minutes := int((until.Sub(now) + time.Minute - 1) / time.Minute)
	return func() tea.Msg {
		d, err := m.client.Snooze(m.ctx, minutes)
		return dndMsg{dnd: d, err: err}
	}
}

// --- :away / :back ------------------------------------------------------------

func (m *Model) cmdAway(away bool) tea.Cmd {
	return func() tea.Msg {
		return awayMsg{away: away, err: m.client.SetAway(m.ctx, away)}
	}
}

// --- rendering ----------------------------------------------------------------

// meStatus is the status-bar part for the user's own state: DND, away and
// their custom status.
func (m *Model) meStatus() []string {
	var parts []string
	if until, on := m.dnd.Until(time.Now()); on {
		parts = append(parts, sWarn.Render(dndIcon+" dnd until "+clock(until, m.cfg.TimeFormat)))
	}
	if m.away {
		parts = append(parts, sDim.Render("away"))
	}
	if line := m.statusLine(m.client.SelfID); line != "" {
		parts = append(parts, sDim.Render(ansi.Truncate(line, 32, "…")))
	}
	return parts
}
