package tui

import (
	"strings"
	"testing"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func TestBotNameLookup(t *testing.T) {
	m := chanWithMessages(t)
	m.focus = fMessages
	_, cmd := m.Update(historyMsg{id: "C2", msgs: []backend.Message{
		{TS: "100.000003", BotID: "B1", SubType: "bot_message", Text: "deploy done"},
	}})
	_ = cmd // would call bots.info; answered by hand below
	if _, asked := m.bots["B1"]; !asked {
		t.Fatal("bot name was not looked up")
	}
	if v := dump(t, m); !strings.Contains(v, "bot APP") || strings.Contains(v, "unknown") {
		t.Error("while loading, show 'bot', never 'unknown'")
	}
	m.Update(botMsg{id: "B1", name: "Builder"})
	if v := dump(t, m); !strings.Contains(v, "Builder APP") {
		t.Error("bot name not shown")
	}
}

// openAtEnd reports a style or hyperlink still active at the end of a row.
func openAtEnd(row string) bool {
	sgr, link := false, false
	for _, m := range reEsc.FindAllStringSubmatch(row, -1) {
		switch seq := m[0]; {
		case strings.HasPrefix(seq, "\x1b]8;"):
			link = m[1] != ""
		case seq == "\x1b[m" || seq == "\x1b[0m":
			sgr = false
		default:
			sgr = true
		}
	}
	return sgr || link
}

func TestWrappedLinkStaysInItsRow(t *testing.T) {
	m := chanWithMessages(t)
	m.focus = fMessages
	long := "https://eu-west-1.console.aws.amazon.com/codesuite/codebuild/782694440109/projects/Builder/build/Builder:024a1abb-e0f9-4485-8fa1-4931a0d21396"
	m.Update(historyMsg{id: "C2", msgs: []backend.Message{
		{TS: "100.000003", User: "U1", Text: "Build and deploy - follow here <" + long + "> and *bold text that wraps over the line for sure because it is long enough*"},
	}})
	v := m.View()
	dump(t, m)
	for i, row := range strings.Split(v, "\n") {
		if openAtEnd(row) {
			t.Errorf("row %d ends with a style or link still open: %q", i, row)
		}
	}
}
