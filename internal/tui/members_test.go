package tui

import (
	"strings"
	"testing"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func TestMembers(t *testing.T) {
	m := fakeModel(t, 110, 24)
	m.open("C1")
	m.users["U1"] = backend.User{ID: "U1", Name: "anna", RealName: "Anna Jensen", Title: "Backend developer"}
	m.runCmd(":members")
	if m.members == nil {
		t.Fatal(":members should open the pane")
	}
	if v := dump(t, m); !strings.Contains(v, "loading…") {
		t.Error("should say it's loading")
	}
	m.update(membersMsg{conv: "C1", ids: []string{"U2", "ME", "U1", "UEXT"}})
	if n := m.scanUsers(); n == nil || !m.userLookups["UEXT"] {
		t.Error("an unknown member should be looked up")
	}
	v := dump(t, m)
	for _, want := range []string{"Members  #dev  4 people", "Anna Jensen  Backend developer", "Erik (you)"} {
		if !strings.Contains(v, want) {
			t.Errorf("pane lacks %q", want)
		}
	}
	if i := strings.Index(v, "Anna Jensen"); i < 0 || i > strings.Index(v, "Bob") {
		t.Error("members should be sorted by name")
	}
	m.update(key("bob"))
	m.update(key("enter"))
	if m.profile == nil || m.profile.id != "U2" {
		t.Errorf("enter should show Bob's profile, got %+v", m.profile)
	}
	m.update(key("esc"))
	if m.profile != nil || m.members == nil {
		t.Error("esc closes the profile, back to the list")
	}
	m.update(key("esc"))
	if m.members != nil {
		t.Error("esc closes the list")
	}
}
