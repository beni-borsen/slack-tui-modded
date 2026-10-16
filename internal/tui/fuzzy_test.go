package tui

import (
	"slices"
	"testing"
)

func TestFuzzyRank(t *testing.T) {
	names := []string{"#general", "Anna Jensen", "#dev-alerts", "#dev", "Bob, Anna Jensen", "#random"}
	get := func(q string) []string {
		var out []string
		for _, i := range fuzzyRank(q, names) {
			out = append(out, names[i])
		}
		return out
	}
	if got := get("dev"); !slices.Equal(got, []string{"#dev", "#dev-alerts"}) {
		t.Errorf("dev: %v", got)
	}
	if got := get("anna"); len(got) != 2 || got[0] != "Anna Jensen" {
		t.Errorf("anna: %v", got)
	}
	if got := get("aj"); len(got) == 0 || got[0] != "Anna Jensen" {
		t.Errorf("aj (initials): %v", got)
	}
	if got := get("DEV"); len(got) != 2 {
		t.Errorf("case-insensitive: %v", got)
	}
	if got := get("xyz"); len(got) != 0 {
		t.Errorf("xyz: %v", got)
	}
	if got := get(""); len(got) != len(names) || got[0] != names[0] {
		t.Errorf("empty query keeps order: %v", got)
	}
}
