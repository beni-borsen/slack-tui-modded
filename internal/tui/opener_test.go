package tui

import (
	"strings"
	"testing"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

// These never run the returned commands, so nothing is really opened.
func TestOpenChooser(t *testing.T) {
	m := chanWithMessages(t)
	m.update(historyMsg{id: "C2", msgs: []backend.Message{
		{TS: "100.000001", User: "U1", Text: "one <https://a.dk/x|a.dk/x>"},
		{TS: "100.000002", User: "U1", Text: "see <https://docs.dk|the docs> and <https://b.dk>",
			Files: []backend.File{{ID: "F1", Name: "plan.pdf", Mimetype: "application/pdf"}}},
	}})
	m.focus = fMessages

	m.Update(key("o")) // newest message: a file and two links
	if m.opener == nil || len(m.opener.items) != 3 {
		t.Fatalf("want a chooser with 3 items, got %+v", m.opener)
	}
	v := dump(t, m)
	for _, want := range []string{"1 plan.pdf  file", "2 the docs  https://docs.dk", "3 https://b.dk"} {
		if !strings.Contains(v, want) {
			t.Errorf("chooser missing %q", want)
		}
	}
	m.Update(key("j"))
	m.Update(key("j"))
	m.Update(key("enter"))
	if m.opener != nil || !strings.Contains(m.status, "opening https://b.dk") {
		t.Errorf("enter should open the third item: opener=%v status=%q", m.opener, m.status)
	}

	m.Update(key("o"))
	m.Update(key("2"))
	if m.opener != nil || !strings.Contains(m.status, "https://docs.dk") {
		t.Errorf("2 should open the docs link: status=%q", m.status)
	}

	m.Update(key("k")) // older message: a single link opens straight away
	m.Update(key("o"))
	if m.opener != nil || !strings.Contains(m.status, "opening https://a.dk/x") {
		t.Errorf("a lone link should open without a chooser: status=%q", m.status)
	}
}
