package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func TestParseStatus(t *testing.T) {
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local)
	cases := []struct {
		in, emoji, text string
		expires         time.Time
		err             bool
	}{
		{in: ":palm_tree: On vacation", emoji: ":palm_tree:", text: "On vacation"},
		{in: ":hamburger: Lunch 45m", emoji: ":hamburger:", text: "Lunch", expires: now.Add(45 * time.Minute)},
		{in: "In a meeting 1h30m", text: "In a meeting", expires: now.Add(90 * time.Minute)},
		{in: ":house: WFH until tomorrow", emoji: ":house:", text: "WFH", expires: time.Date(2026, 10, 20, 0, 0, 0, 0, time.Local)},
		{in: "Focus until 14:30", text: "Focus", expires: time.Date(2026, 10, 19, 14, 30, 0, 0, time.Local)},
		{in: "Early start until 8:00", text: "Early start", expires: time.Date(2026, 10, 20, 8, 0, 0, 0, time.Local)},
		{in: ":car: 2d", emoji: ":car:", expires: now.Add(48 * time.Hour)},
		{in: "back in 5", text: "back in 5"},               // a bare number stays text
		{in: "Meeting at 14:00", text: "Meeting at 14:00"}, // so does a time without "until"
		{in: ":thumbsup::skin-tone-3: ok", emoji: ":thumbsup::skin-tone-3:", text: "ok"},
		{in: "  ", err: true},
		{in: strings.Repeat("x", 101), err: true},
	}
	for _, c := range cases {
		emoji, text, exp, err := parseStatus(c.in, now)
		if (err != nil) != c.err {
			t.Errorf("%q: err %v", c.in, err)
			continue
		}
		if emoji != c.emoji || text != c.text || !exp.Equal(c.expires) {
			t.Errorf("%q: got (%q, %q, %v), want (%q, %q, %v)", c.in, emoji, text, exp, c.emoji, c.text, c.expires)
		}
	}
}

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"30m": 30 * time.Minute, "2h": 2 * time.Hour, "1h30m": 90 * time.Minute, "1d": 24 * time.Hour,
		"1w": 7 * 24 * time.Hour, "45": 45 * time.Minute, "2H": 2 * time.Hour,
	} {
		if got, ok := parseDuration(in); !ok || got != want {
			t.Errorf("%q = %v %v, want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "0", "0m", "abc", "1x", "-5", "m"} {
		if _, ok := parseDuration(in); ok {
			t.Errorf("%q should not parse", in)
		}
	}
}

func TestDNDState(t *testing.T) {
	now := time.Date(2026, 10, 19, 23, 0, 0, 0, time.Local)
	sched := backend.DND{Start: now.Add(-time.Hour), End: now.Add(8 * time.Hour)}
	if until, on := sched.Until(now); !on || !until.Equal(sched.End) {
		t.Errorf("inside quiet hours: %v %v", until, on)
	}
	if _, on := sched.Until(now.Add(-2 * time.Hour)); on {
		t.Error("before quiet hours should be off")
	}
	// A snooze that runs past the scheduled end wins.
	both := sched
	both.SnoozeEnd = now.Add(10 * time.Hour)
	if until, _ := both.Until(now); !until.Equal(both.SnoozeEnd) {
		t.Errorf("snooze past the window: until %v", until)
	}
	if next := both.Next(now); !next.Equal(sched.End) {
		t.Errorf("next change %v, want the window's end", next)
	}
	if !(backend.DND{}).Next(now).IsZero() {
		t.Error("no DND, no next change")
	}
}

func TestCommandLine(t *testing.T) {
	m := fakeModel(t, 100, 24)
	m.Update(key(":"))
	if m.cmd == nil {
		t.Fatal(": should open the command line")
	}
	typeText(m, "st")
	if c := m.cmd.complete; c == nil || len(c.items) != 1 || c.items[0].insert != "status" {
		t.Fatalf("want status suggested, got %+v", m.cmd.complete)
	}
	v := dump(t, m)
	if !strings.Contains(v, ":st") || !strings.Contains(v, "set your status") {
		t.Errorf("command line or suggestions missing:\n%s", v)
	}
	m.handleKey(tabKey())
	if got := m.cmd.input.Value(); got != "status " {
		t.Fatalf("tab gave %q", got)
	}
	typeText(m, ":palm")
	if c := m.cmd.complete; c == nil || c.items[0].emoji != "palm_tree" {
		t.Fatalf("want palm_tree suggested, got %+v", m.cmd.complete)
	}
	m.handleKey(tabKey())
	typeText(m, "Away")
	if got := m.cmd.input.Value(); got != "status :palm_tree: Away" {
		t.Fatalf("input %q", got)
	}
	if cmd := m.handleKey(key("enter")); cmd == nil || m.cmd != nil {
		t.Fatalf("enter should run it and close: cmd=%v open=%v", cmd != nil, m.cmd != nil)
	}

	// History: ↑ brings the last command back.
	m.Update(key(":"))
	m.handleKey(key("up"))
	if got := m.cmd.input.Value(); got != "status :palm_tree: Away" {
		t.Errorf("history gave %q", got)
	}
	m.handleKey(key("esc"))
	if m.cmd != nil {
		t.Error("esc should close")
	}

	// A unique prefix runs the command; unknown ones say so.
	m.runCmd("sta :not_an_emoji_at_all: hi")
	if !m.statusErr || !strings.Contains(m.status, "unknown emoji") {
		t.Errorf("status %q", m.status)
	}
	m.runCmd("frobnicate")
	if !m.statusErr || !strings.Contains(m.status, "unknown command :frobnicate") {
		t.Errorf("status %q", m.status)
	}
	m.runCmd("dnd soon")
	if !m.statusErr || !strings.Contains(m.status, "usage: :dnd") {
		t.Errorf("status %q", m.status)
	}
	if m.runCmd("dnd 30") == nil || m.runCmd("dnd until 17:00") == nil {
		t.Error("dnd with a duration should call Slack")
	}
	m.runCmd("dnd off")
	if !strings.Contains(m.status, "already on") {
		t.Errorf("status %q", m.status)
	}
}

func TestStatusAndPresenceShown(t *testing.T) {
	m := fakeModel(t, 120, 24)
	soon := time.Now().Add(time.Hour)
	m.Update(statusSetMsg{emoji: ":palm_tree:", text: "Vacation"})
	m.Update(dndMsg{dnd: backend.DND{SnoozeEnd: soon}})
	m.Update(presenceMsg{active: map[string]bool{"U1": true, "U2": false}})
	u := m.users["U1"]
	u.StatusEmoji, u.StatusText = ":house:", "WFH"
	m.Update(backend.EvUserChanged{User: u})
	b := m.users["U2"]
	b.StatusEmoji, b.StatusText, b.StatusExpires = ":car:", "Driving", time.Now().Add(-time.Minute).Unix()
	m.Update(backend.EvUserChanged{User: b})

	prof := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256) // so styles emit escapes
	raw := m.View()
	lipgloss.SetColorProfile(prof)
	v := dump(t, m)
	status := strings.Split(v, "\n")[m.height-1]
	for _, want := range []string{"dnd until " + soon.Format("15:04"), "🌴 Vacation"} {
		if !strings.Contains(status, want) {
			t.Errorf("status bar lacks %q: %q", want, status)
		}
	}
	if !strings.Contains(v, "Anna Jensen 🏠") {
		t.Errorf("Anna's status emoji missing from the sidebar")
	}
	if strings.Contains(v, "🚗") {
		t.Errorf("Bob's expired status still shown")
	}
	// Anna is active: her icon is green (colour 114).
	green := 0
	for _, l := range strings.Split(raw, "\n") {
		if strings.Contains(ansi.Strip(l), userIcon+" Anna Jensen") {
			if !strings.Contains(l, "38;5;114m"+userIcon) {
				t.Errorf("Anna's icon not green: %q", l)
			}
			green++
		}
	}
	if green == 0 {
		t.Error("Anna's DM row not found")
	}

	m.open("D1")
	v = dump(t, m)
	if !strings.Contains(v, "Anna Jensen  active  🏠 WFH") {
		t.Errorf("DM header lacks presence and status")
	}

	// When the snooze ends, Slack's dnd_updated clears it.
	m.Update(backend.EvDND{DND: backend.DND{}})
	if strings.Contains(dump(t, m), "dnd until") {
		t.Error("dnd still shown")
	}
}

func TestPresenceUsers(t *testing.T) {
	m := fakeModel(t, 100, 24)
	got := m.presenceUsers()
	// Anna (in the Team section) and Bob; never the user themselves.
	if strings.Join(got, ",") != "U1,U2" {
		t.Errorf("got %v", got)
	}
	m.presence["U1"] = true
	if m.presenceFor("D1") != nil {
		t.Error("known presence fetched again")
	}
	if m.presenceFor("D2") == nil {
		t.Error("unknown presence not fetched on open")
	}
	m.cfg.PresenceMinutes = -1
	if m.pollPresence() != nil || m.presenceFor("D2") != nil {
		t.Error("presence_minutes < 0 should turn polling off")
	}
}

var _ tea.Msg = presenceTick{}
