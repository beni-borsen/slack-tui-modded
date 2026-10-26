package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

var testPublic = []backend.PublicChannel{
	{ID: "C1", Name: "dev", Members: 12, IsMember: true},
	{ID: "C2", Name: "general", Members: 80, IsMember: true},
	{ID: "C7", Name: "design", Members: 1, Purpose: "Mockups\nand reviews"},
	{ID: "C9", Name: "random", Members: 40},
}

func TestJoinBrowser(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.runCmd("join")
	if m.chans == nil || !m.publicLoading {
		t.Fatal(":join alone should open the browser and fetch the channels")
	}
	if !strings.Contains(dump(t, m), "loading…") {
		t.Error("no loading placeholder")
	}
	m.Update(publicChansMsg{chans: append([]backend.PublicChannel(nil), testPublic...)})
	v := dump(t, m)
	for _, want := range []string{"4 public", "#design  1 member · Mockups and reviews", "#dev  joined · 12 members"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, k := range []string{"d", "e"} {
		m.Update(key(k))
	}
	if got := m.public[m.chans.matches[0]].Name; got != "dev" || len(m.chans.matches) != 2 {
		t.Errorf("filter \"de\": first %s, %d matches", got, len(m.chans.matches))
	}
	m.Update(key("down"))
	m.Update(key("enter"))
	if m.chans != nil || !strings.Contains(m.status, "joining #design") {
		t.Errorf("enter should join #design: status %q", m.status)
	}
	m.Update(joinedMsg{conv: backend.Conversation{ID: "C7", Name: "design", Kind: backend.KindChannel}, name: "design"})
	if m.current != "C7" || !m.public[2].IsMember || !strings.Contains(m.status, "joined #design") {
		t.Errorf("after joining: current %s status %q", m.current, m.status)
	}

	// Enter on a channel we are in just opens it.
	m.runCmd("join")
	m.Update(key("enter"))
	if m.current != "C1" || m.chans != nil {
		t.Errorf("current %s", m.current)
	}
}

func TestJoinByName(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.runCmd("join #dev")
	if m.current != "C1" || m.chans != nil {
		t.Error(":join of a channel we are in should open it")
	}
	// Not loaded yet: the browser opens filtered, and joins once the list
	// shows the channel exists.
	m.runCmd("join Random")
	if m.chans == nil || m.chans.input.Value() != "random" {
		t.Fatal("browser not opened with the name")
	}
	m.Update(publicChansMsg{chans: append([]backend.PublicChannel(nil), testPublic...)})
	if m.chans != nil || !strings.Contains(m.status, "joining #random") {
		t.Errorf("should join at once: status %q", m.status)
	}
	// Loaded, and no such channel: the browser stays.
	m.runCmd("join nope")
	if m.chans == nil || len(m.chans.matches) != 0 || !strings.Contains(dump(t, m), "no channel matches") {
		t.Error("an unknown name should leave the browser open")
	}
	m.Update(key("esc"))
	m.Update(joinedMsg{name: "random", err: errors.New("conversations.join: is_archived")})
	if !m.statusErr || m.status != "couldn't join #random: it's archived" {
		t.Errorf("status %q", m.status)
	}
}

func TestLeave(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.open("C1")
	m.runCmd("leave")
	if m.confirm == nil || m.confirm.prompt != "leave #dev? y/n" {
		t.Fatalf("confirm %+v", m.confirm)
	}
	m.Update(key("n"))
	if m.convs["C1"] == nil {
		t.Fatal("n should not leave")
	}
	m.runCmd("leave #secret-project")
	if m.confirm == nil || !strings.Contains(m.confirm.prompt, "only an invite brings you back") {
		t.Errorf("leaving a private channel should warn: %+v", m.confirm)
	}
	m.confirm = nil
	m.open("D1")
	m.runCmd("leave")
	if m.confirm != nil || !strings.Contains(m.status, "can't be left") {
		t.Errorf("DM: status %q", m.status)
	}
	m.runCmd("leave #nope")
	if m.status != "you're not in #nope" {
		t.Errorf("status %q", m.status)
	}

	m.open("C1")
	m.Update(leftMsg{id: "C1", name: "#dev", err: errors.New("conversations.leave: cant_leave_general")})
	if m.convs["C1"] == nil || !strings.Contains(m.status, "nobody can leave") {
		t.Errorf("a failed leave should keep the channel: status %q", m.status)
	}
	m.Update(leftMsg{id: "C1", name: "#dev"})
	if m.convs["C1"] != nil || m.current != unreadsID || m.status != "left #dev" {
		t.Errorf("after leaving: current %s status %q", m.current, m.status)
	}
	if strings.Contains(dump(t, m), "# dev") {
		t.Error("#dev still in the sidebar")
	}
}

func TestLeftElsewhere(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.open("C2")
	var convs []backend.Conversation
	for _, c := range m.convs {
		if c.ID != "C2" {
			convs = append(convs, *c)
		}
	}
	m.Update(convsMsg{convs: convs})
	if m.convs["C2"] != nil || m.current != unreadsID {
		t.Errorf("a channel left in Slack should close here: current %s", m.current)
	}
}

func TestCreate(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.runCmd("create")
	if m.confirm != nil || !strings.Contains(m.status, "usage") {
		t.Errorf("status %q", m.status)
	}
	m.runCmd("create #Dev")
	if m.confirm != nil || !strings.Contains(m.status, "already exists") {
		t.Errorf("status %q", m.status)
	}
	m.runCmd("create --private Launch Plans")
	if m.confirm == nil || m.confirm.prompt != "create private channel #launch-plans? y/n" {
		t.Fatalf("confirm %+v", m.confirm)
	}
	m.confirm = nil
	m.runCmd("create launch")
	if m.confirm == nil || m.confirm.prompt != "create public channel #launch? y/n" {
		t.Fatalf("confirm %+v", m.confirm)
	}
	m.confirm = nil
	m.Update(createdMsg{name: "launch", err: errors.New("conversations.create: name_taken")})
	if !strings.Contains(m.status, ":join launch") {
		t.Errorf("status %q", m.status)
	}
	m.Update(createdMsg{name: "launch", conv: backend.Conversation{ID: "C5", Name: "launch", Kind: backend.KindChannel}})
	if m.current != "C5" || !strings.Contains(m.status, "created #launch") {
		t.Errorf("current %s status %q", m.current, m.status)
	}
}

func TestInvite(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.runCmd("invite @anna")
	if !strings.Contains(m.status, "open a channel first") {
		t.Errorf("status %q", m.status)
	}
	m.open("C1")
	for line, want := range map[string]string{
		"invite":        "usage: :invite @person [@person…]",
		"invite @zed":   "nobody called @zed",
		"invite @erik":  "@erik is you",
		"invite @Bob x": "nobody called @x",
	} {
		m.runCmd(line)
		if m.status != want {
			t.Errorf("%s: status %q, want %q", line, m.status, want)
		}
	}
	if cmd := m.runCmd("invite @anna Bob"); cmd == nil {
		t.Error("handle and display name should both resolve")
	}
	m.Update(invitedMsg{conv: "C1", names: []string{"@anna", "@bob"}})
	if m.status != "invited @anna, @bob to #dev" {
		t.Errorf("status %q", m.status)
	}
	m.Update(invitedMsg{conv: "C1", names: []string{"@anna"}, err: errors.New("conversations.invite: already_in_channel")})
	if m.status != "couldn't invite to #dev: already in it" {
		t.Errorf("status %q", m.status)
	}
}

func TestChannelCompletion(t *testing.T) {
	m := fakeModel(t, 100, 20)
	m.open("C1")
	m.Update(key(":"))
	for _, k := range []string{"j", "o", "i", "n", " "} {
		m.Update(key(k))
	}
	if !m.publicLoading || m.cmd.complete != nil {
		t.Fatal(":join should fetch the channel list for its suggestions")
	}
	m.Update(publicChansMsg{chans: append([]backend.PublicChannel(nil), testPublic...)})
	if c := m.cmd.complete; c == nil || len(c.items) != 2 || c.items[0].insert != "#design" {
		t.Fatalf("suggestions %+v", m.cmd.complete)
	}
	m.Update(key("r"))
	m.Update(key("tab"))
	if v := m.cmd.input.Value(); v != "join #random " {
		t.Errorf("input %q", v)
	}

	m.cmd.input.SetValue("")
	for _, k := range []string{"i", "n", "v", " ", "@", "a"} {
		m.Update(key(k))
	}
	c := m.cmd.complete
	if c == nil || c.items[0].insert != "@anna" {
		t.Fatalf("suggestions %+v", c)
	}
	for _, it := range c.items {
		if it.insert == "@erik" || strings.HasPrefix(it.token, "<!") {
			t.Errorf("suggested %s", it.insert)
		}
	}
	m.Update(key("enter"))
	if v := m.cmd.input.Value(); v != "inv @anna " {
		t.Errorf("input %q", v)
	}
}
