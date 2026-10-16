package main

import (
	"testing"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func TestMatchPerson(t *testing.T) {
	users := []backend.User{
		{ID: "U1", Name: "benjamin.h", DisplayName: "Benjamin", RealName: "Benjamin Hansen"},
		{ID: "U2", Name: "bjo", DisplayName: "Bjørn", RealName: "Bjørn Olsen"},
		{ID: "U3", Name: "anna", RealName: "Anna Benjaminsen"},
		{ID: "U4", Name: "old", DisplayName: "Benjamin", Deleted: true},
	}
	ids := func(us []backend.User) (out []string) {
		for _, u := range us {
			out = append(out, u.ID)
		}
		return
	}
	for name, want := range map[string]string{
		"Benjamin": "U1",    // exact display name beats Anna Benjaminsen's surname prefix
		"benjamin": "U1",    // case-insensitive
		"bjørn":    "U2",    // non-ASCII
		"olsen":    "U2",    // a word in the full name
		"benj":     "U1,U3", // ambiguous: both listed
		"nobody":   "",
	} {
		got := ""
		for i, id := range ids(matchPerson(users, name)) {
			if i > 0 {
				got += ","
			}
			got += id
		}
		if got != want {
			t.Errorf("matchPerson(%q) = %q, want %q", name, got, want)
		}
	}
}
