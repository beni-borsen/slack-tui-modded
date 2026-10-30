package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/config"
)

func TestModeEnd(t *testing.T) {
	now := time.Date(2026, 10, 30, 14, 5, 0, 0, time.Local)
	for until, want := range map[string]time.Time{
		"":      time.Date(2026, 10, 31, 0, 0, 0, 0, time.Local),
		"08:00": time.Date(2026, 10, 31, 8, 0, 0, 0, time.Local),
	} {
		if got, err := modeEnd(until, now); err != nil || !got.Equal(want) {
			t.Errorf("modeEnd(%q) = %v, %v; want %v", until, got, err, want)
		}
	}
	if _, err := modeEnd("8am", now); err == nil {
		t.Error("8am should be refused")
	}
}

// responder is the fake model with a first-responder mode watching #dev,
// turned on, and #general open.
func responder(t *testing.T) *Model {
	m := fakeModel(t, 120, 26)
	m.cfg.Modes = []config.Mode{{Name: "first-responder", Watch: []string{"#dev"}}}
	m.open("C2")
	m.runCmd(":first-responder")
	if m.mode == nil {
		t.Fatal(":first-responder should turn the mode on")
	}
	return m
}

func TestModeToggleAndState(t *testing.T) {
	m := responder(t)
	if st := config.LoadState(); st.Mode != "first-responder" || st.ModeUntil != m.mode.until.Unix() {
		t.Errorf("state = %+v, want the mode saved", st)
	}
	if v := dump(t, m); !strings.Contains(v, "⚑ first-responder") {
		t.Error("the status bar should show the mode")
	}
	m.runCmd("first-responder")
	if m.mode != nil {
		t.Error("the same command again should turn it off")
	}
	if st := config.LoadState(); st.Mode != "" {
		t.Errorf("state = %+v, want the mode gone", st)
	}
}

func TestModeRestore(t *testing.T) {
	m := responder(t)
	m.mode = nil
	st := config.LoadState()
	m.restoreMode(st)
	if m.mode == nil {
		t.Fatal("a mode that hasn't run out should come back")
	}
	m.mode = nil
	st.ModeUntil = time.Now().Add(-time.Minute).Unix()
	m.restoreMode(st)
	if m.mode != nil || config.LoadState().Mode != "" {
		t.Error("a mode that has run out should be dropped")
	}
}

func TestModeAlertSwitches(t *testing.T) {
	m := responder(t)
	m.update(backend.EvMessage{Channel: "C1", Msg: backend.Message{TS: "400.0", User: "U2", Text: "Monitor is DOWN"}})
	if m.current != "C1" || m.alert == nil {
		t.Fatalf("current %s, alert %v: want #dev opened with an alert", m.current, m.alert)
	}
	v := dump(t, m)
	if !strings.Contains(v, "┏") || !strings.Contains(v, "⚠ #dev") {
		t.Error("the panes should be framed for the alert and the bar should name it")
	}
	m.update(key("j"))
	if m.alert != nil {
		t.Error("a key while looking at the channel should see the alert")
	}
	if v := dump(t, m); strings.Contains(v, "┏") {
		t.Error("the frame should go back to normal")
	}
}

func TestModeAlertWaitsWhileTyping(t *testing.T) {
	m := responder(t)
	m.update(key("i"))
	m.update(key("half a thought"))
	m.update(backend.EvMessage{Channel: "C1", Msg: backend.Message{TS: "400.0", User: "U2", Text: "DOWN"}})
	if m.current != "C2" || m.alert == nil {
		t.Fatalf("current %s, alert %v: want to stay put with an alert", m.current, m.alert)
	}
	if v := dump(t, m); !strings.Contains(v, "! go") {
		t.Error("the bar should offer ! to go there")
	}
	m.update(key("esc"))
	if m.input.Value() != "half a thought" {
		t.Errorf("the draft should be untouched: %q", m.input.Value())
	}
	m.update(key("!"))
	if m.current != "C1" || m.alert != nil {
		t.Errorf("! should go to #dev and see the alert; current %s", m.current)
	}
}

func TestModeIgnores(t *testing.T) {
	m := responder(t)
	for _, ev := range []backend.EvMessage{
		{Channel: "C1", Msg: backend.Message{TS: "401.0", ThreadTS: "400.0", User: "U2", Text: "reply"}},
		{Channel: "C1", Msg: backend.Message{TS: "402.0", User: "ME", Text: "mine"}},
		{Channel: "C1", Msg: backend.Message{TS: "403.0", User: "U2", SubType: "channel_join"}},
		{Channel: "C3", Msg: backend.Message{TS: "404.0", User: "U2", Text: "not watched"}},
	} {
		m.update(ev)
		if m.alert != nil || m.current != "C2" {
			t.Errorf("%+v should not alert", ev.Msg)
			m.alert, m.current = nil, "C2"
		}
	}
	m.runCmd("mode off")
	m.update(backend.EvMessage{Channel: "C1", Msg: backend.Message{TS: "405.0", User: "U2", Text: "DOWN"}})
	if m.alert != nil {
		t.Error("no alerts once the mode is off")
	}
}
