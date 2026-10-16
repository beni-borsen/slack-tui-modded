package backend

import (
	"net/url"
	"testing"
)

func TestSlackHost(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://files.slack.com/files-pri/T1-F1/x.png": true,
		"https://slack.com/x":                           true,
		"http://files.slack.com/x":                      false, // no plain http
		"https://files.slack.com.evil.io/x":             false,
		"https://evilslack.com/x":                       false,
		"https://example.com/?u=files.slack.com":        false,
	} {
		u, _ := url.Parse(raw)
		if got := slackHost(u); got != want {
			t.Errorf("slackHost(%s) = %v", raw, got)
		}
	}
}
