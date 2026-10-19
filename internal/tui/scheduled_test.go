package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func TestParseLater(t *testing.T) {
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local) // a Monday
	at := func(d, h, mi int) time.Time { return time.Date(2026, 10, d, h, mi, 0, 0, time.Local) }
	ok := map[string]time.Time{
		"30m":           now.Add(30 * time.Minute),
		"in 1h30m":      now.Add(90 * time.Minute),
		"15:30":         at(19, 15, 30),
		"3pm":           at(19, 15, 0),
		"3 pm":          at(19, 15, 0),
		"3":             at(19, 15, 0), // a bare hour already past: the afternoon
		"9:00":          at(20, 9, 0),  // past with minutes: tomorrow
		"tomorrow":      at(20, 9, 0),
		"tomorrow 8:30": at(20, 8, 30),
		"tomorrow at 2": at(20, 14, 0),
		"Fri":           at(23, 9, 0),
		"friday 14":     at(23, 14, 0),
		"mon":           at(26, 9, 0), // today's 9:00 has passed
		"mon 11":        at(19, 11, 0),
		"today 17:45":   at(19, 17, 45),
		"tmrw 7am":      at(20, 7, 0),
	}
	for in, want := range ok {
		got, err := parseLater(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseLater(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "soon", "today 8:00", "200d", "next week", "fri 25:00", "30"} {
		if _, err := parseLater(in, now); err == nil {
			t.Errorf("parseLater(%q) should fail", in)
		}
	}
}

func TestFmtWhen(t *testing.T) {
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local)
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(time.Hour), "today 11:00"},
		{now.Add(23 * time.Hour), "tomorrow 09:00"},
		{now.AddDate(0, 0, 4), "Fri 10:00"},
		{now.AddDate(0, 0, 10), "Thu 29 Oct 10:00"},
	} {
		if got := fmtWhen(c.t, now, "15:04"); got != c.want {
			t.Errorf("fmtWhen(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestLaterFromComposer(t *testing.T) {
	m := chanWithMessages(t)
	m.handleKey(key(":"))
	typeText(m, "later 30m")
	m.handleKey(key("enter"))
	if !strings.Contains(m.status, "write the message first") {
		t.Errorf("an empty composer should be refused, status %q", m.status)
	}

	m.handleKey(key("i"))
	typeText(m, "standup notes")
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.cmd == nil || m.cmd.input.Value() != "later " {
		t.Fatalf("ctrl+s should open the command line with later, got %+v", m.cmd)
	}
	m.handleKey(key("esc"))
	if !m.insert || m.input.Value() != "standup notes" {
		t.Fatal("esc from the command line should go back to writing")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	typeText(m, "whenever")
	m.handleKey(key("enter"))
	if m.input.Value() != "standup notes" || !m.statusErr {
		t.Fatal("a bad time should keep the text and say so")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	typeText(m, "30m")
	if cmd := m.handleKey(key("enter")); cmd == nil {
		t.Fatal("no schedule call")
	}
	if m.input.Value() != "" || m.insert {
		t.Error("scheduling should clear the composer and leave insert mode")
	}

	// It failed: the text comes back.
	m.Update(scheduledMsg{text: "standup notes", err: errors.New("chat.scheduleMessage: time_in_past")})
	if m.input.Value() != "standup notes" {
		t.Error("a failed schedule lost the text")
	}
	m.input.Reset()

	// It worked: the header counts it.
	at := time.Now().Add(30 * time.Minute)
	m.Update(scheduledMsg{s: backend.Scheduled{ID: "Q1", Channel: "C2", Text: "standup notes", PostAt: at}})
	if !strings.Contains(m.status, "scheduled for today") {
		t.Errorf("status %q", m.status)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, schedIcon+" 1 scheduled") {
		t.Errorf("the header should count scheduled messages:\n%s", v)
	}
	// Once due it has been sent; the count goes.
	m.scheduled[0].PostAt = time.Now().Add(-time.Second)
	if v := ansi.Strip(m.View()); strings.Contains(v, "1 scheduled") {
		t.Error("a due message still counted")
	}
}

func TestScheduledPane(t *testing.T) {
	m := chanWithMessages(t)
	m.runCmd("sc")
	if m.sched == nil || !m.sched.loading {
		t.Fatal(":sc should open the scheduled pane and load it")
	}
	for _, k := range []string{"e", "d", "enter", "G"} { // nothing to act on yet
		m.handleKey(key(k))
	}
	if m.confirm != nil || m.sched == nil {
		t.Fatal("keys on an empty list did something")
	}
	soon, later := time.Now().Add(time.Hour), time.Now().Add(48*time.Hour)
	m.Update(scheduledListMsg{list: []backend.Scheduled{
		{ID: "Q1", Channel: "C2", Text: "ping <@U1>", PostAt: soon},
		{ID: "Q2", Channel: "C1", Text: "weekly report", PostAt: later},
		{ID: "Q0", Channel: "C1", Text: "already sent", PostAt: time.Now().Add(-time.Minute)},
	}})
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "ping @Anna") || !strings.Contains(v, "weekly report") || strings.Contains(v, "already sent") {
		t.Fatalf("pane:\n%s", v)
	}

	// Cancel the second, but it fails: it comes back.
	m.handleKey(key("j"))
	m.handleKey(key("d"))
	if m.confirm == nil {
		t.Fatal("d should ask first")
	}
	if m.handleKey(key("y")) == nil || len(m.upcoming()) != 1 {
		t.Fatal("y should cancel it at once")
	}
	m.Update(unscheduledMsg{s: backend.Scheduled{ID: "Q2", Channel: "C1", Text: "weekly report", PostAt: later}, err: errors.New("chat.deleteScheduledMessage: invalid_scheduled_message_id")})
	if len(m.upcoming()) != 2 || m.upcoming()[1].ID != "Q2" {
		t.Fatalf("a failed cancel should put it back, got %+v", m.upcoming())
	}

	// e: back into the composer, once Slack has dropped it.
	m.handleKey(key("g"))
	if m.handleKey(key("e")) == nil || len(m.upcoming()) != 1 {
		t.Fatal("e should unschedule")
	}
	m.Update(unscheduledMsg{s: backend.Scheduled{ID: "Q1", Channel: "C2", Text: "ping <@U1>", PostAt: soon}, toInput: true})
	if m.sched != nil || !m.insert || m.current != "C2" || m.input.Value() != "ping @Anna Jensen" {
		t.Fatalf("e should open C2 with the text in the composer: sched=%v insert=%v current=%s input=%q",
			m.sched != nil, m.insert, m.current, m.input.Value())
	}
}
