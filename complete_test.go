package main

import (
	"strings"
	"testing"

	"github.com/magnusdalkvist/slack-tui/internal/names"
)

func TestCandidates(t *testing.T) {
	c := names.Cache{
		People: []names.Person{
			{ID: "U1", Name: "benjamin.h", DisplayName: "Benjamin", RealName: "Benjamin Hansen"},
			{ID: "U2", Name: "erik.larsen", DisplayName: "Erik Larsen 'erla'", RealName: "Erik Larsen"},
			{ID: "U3", Name: "anna", RealName: "Anna Benjaminsen"},
			{ID: "U4", Name: "bo", DisplayName: "Bo"},
			{ID: "U5", Name: "bo2", DisplayName: "Bo"},  // same display name as U4
			{ID: "U6", Name: "beno", RealName: "Benno"}, // no display name: the full name is what Slack shows
		},
		Channels: []string{"digital-udvikling", "general", "dev"},
	}
	for word, want := range map[string]string{
		"@Benj": "@Benjamin",                    // display name starting with it wins; Anna's surname would shorten the word
		"@eri":  "@erik.larsen",                 // display name has spaces/quotes: username instead
		"@lars": "@erik.larsen",                 // a later word of the name
		"@bo":   "@bo,@bo2",                     // shared display name: usernames keep them apart
		"@zz":   "",                             //
		"digi":  "digital-udvikling",            //
		"udv":   "digital-udvikling",            // word inside a channel name
		"d":     "doctor,dev,digital-udvikling", // subcommands first
		"":      "dev,digital-udvikling,general",
	} {
		got := strings.Join(candidates(c, word), ",")
		if got != want {
			t.Errorf("candidates(%q) = %q, want %q", word, got, want)
		}
	}
}
