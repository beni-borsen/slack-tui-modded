package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func TestProfileCard(t *testing.T) {
	m := chanWithMessages(t)
	m.update(key("k")) // Anna's message
	m.update(key("I"))
	if m.profile == nil || m.profile.id != "U1" {
		t.Fatalf("I should open the sender's profile, got %+v", m.profile)
	}
	m.update(profileMsg{user: backend.User{
		ID: "U1", Name: "anna", RealName: "Anna Jensen", DisplayName: "anna.j", Title: "Backend developer",
		Pronouns: "she/her", Email: "anna@example.com", TZ: "Asia/Tokyo", TZLabel: "Japan Standard Time", TZOffset: 9 * 3600,
		StatusEmoji: ":palm_tree:", StatusText: "on holiday",
	}})
	m.presence["U1"] = true
	v := dump(t, m)
	for _, want := range []string{"Anna Jensen", "anna.j · @anna · she/her", "Backend developer", "on holiday", "Japan Standard Time", "anna@example.com", "c copy email"} {
		if !strings.Contains(v, want) {
			t.Errorf("card lacks %q", want)
		}
	}
	m.update(key("enter"))
	if m.profile != nil || m.current != "D1" {
		t.Errorf("enter should write to Anna; current %q", m.current)
	}

	m.runCmd(":profile")
	if m.profile == nil || m.profile.id != "U1" {
		t.Error(":profile in a DM shows the other person")
	}
	m.update(key("esc"))
	m.runCmd(":profile @bob")
	if m.profile == nil || m.profile.id != "U2" {
		t.Error(":profile @bob")
	}
}

func TestLocalTime(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	tokyo := backend.User{TZ: "Asia/Tokyo", TZLabel: "Japan Standard Time", TZOffset: 9 * 3600}
	got := localTime(tokyo, now, "15:04")
	if !strings.HasPrefix(got, "19:00") || !strings.Contains(got, "7 h ahead") {
		t.Errorf("Tokyo: %q", got)
	}
	india := backend.User{TZ: "Asia/Kolkata", TZOffset: 19800}
	if got := localTime(india, now, "15:04"); !strings.Contains(got, "3 h 30 min ahead") {
		t.Errorf("India: %q", got)
	}
}
