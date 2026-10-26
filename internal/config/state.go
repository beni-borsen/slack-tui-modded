package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// State is UI state remembered between runs (not user configuration).
type State struct {
	Folded        []string `json:"folded"`         // sidebar sections that are collapsed
	Unfolded      []string `json:"unfolded"`       // sections collapsed by default (Apps) that are open
	Muted         []string `json:"muted"`          // conversation IDs muted here (Slack's API can't read or set mutes)
	SidebarRight  bool     `json:"sidebar_right"`  // sidebar on the right instead of the left
	SidebarHidden bool     `json:"sidebar_hidden"` // sidebar not shown
}

func statePath() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "slack-tui", "state.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "slack-tui", "state.json")
}

// LoadState reads the state file; a missing or broken file gives empty state.
func LoadState() State {
	var s State
	if b, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func SaveState(s State) error {
	p := statePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}
