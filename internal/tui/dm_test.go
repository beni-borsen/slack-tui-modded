package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func TestDMFindsExisting(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.runCmd(":dm @anna")
	if m.current != "D1" {
		t.Errorf(":dm @anna opened %q, want D1", m.current)
	}
	m.runCmd(":dm bob @anna")
	if m.current != "G1" {
		t.Errorf(":dm bob @anna opened %q, want the group DM G1", m.current)
	}
	m.runCmd(":dm @erik")
	if m.status != "opening Erik…" {
		t.Errorf(":dm yourself should open the self-DM; status %q", m.status)
	}
}

func TestDMStartsNew(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.users["U3"] = backend.User{ID: "U3", Name: "carl", RealName: "Carl Berg"}
	if cmd := m.runCmd(":dm @carl"); cmd == nil || m.status != "opening Carl Berg…" {
		t.Fatalf("status %q; want the conversation being opened", m.status)
	}
	m.update(dmOpenedMsg{conv: backend.Conversation{ID: "D3", Name: "U3", UserID: "U3", Kind: backend.KindDM, InfoLoaded: true}})
	if m.current != "D3" || m.convs["D3"] == nil {
		t.Errorf("current %q; want the new DM open", m.current)
	}
	if v := dump(t, m); !strings.Contains(v, "Carl Berg") {
		t.Error("the new DM should be in the sidebar")
	}
}

func TestSwitcherOffersPeople(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.users["U3"] = backend.User{ID: "U3", Name: "carl", DisplayName: "Carl", RealName: "Carl Berg"}
	m.users["B1"] = backend.User{ID: "B1", Name: "deploybot", IsBot: true}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	for _, id := range m.switcher.matches {
		if strings.HasPrefix(id, personPrefix) {
			t.Fatalf("people without a DM should only show once something is typed; got %q", id)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("berg")})
	if v := dump(t, m); !strings.Contains(v, "Carl  Carl Berg") {
		t.Error("Carl should be offered")
	}
	for _, id := range m.switcher.matches {
		if id == personPrefix+"B1" || id == personPrefix+"U1" {
			t.Errorf("%q shouldn't be offered (a bot, or someone we have a DM with)", id)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.switcher != nil || m.status != "opening Carl…" {
		t.Errorf("enter on Carl: status %q", m.status)
	}
}
