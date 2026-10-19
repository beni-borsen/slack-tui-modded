// Package config loads ~/.config/slack-tui/config.toml.
package config

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Section is a custom sidebar section. Slack's own sections are not in the
// public API, so they are defined here instead.
type Section struct {
	Name string `toml:"name"`
	// Items are "#channel" for channels and "@name" for DMs, where name is a
	// username, display name or real name.
	Items []string `toml:"items"`
	// Icon is an emoji shortcode shown before the name, custom ones too
	// (":partyparrot:").
	Icon string `toml:"icon"`
}

type Config struct {
	Sections []Section `toml:"section"`
	// TimeFormat is a Go time layout for message timestamps.
	TimeFormat string `toml:"time_format"`
	// SidebarWidth is the width of the left column in cells.
	SidebarWidth int `toml:"sidebar_width"`
	// RefreshMinutes is how often unread markers are re-checked against
	// Slack, to pick up messages read on another device.
	RefreshMinutes int `toml:"refresh_minutes"`
	// PresenceMinutes is how often the presence of the people in the
	// sidebar's DMs is checked; negative turns it off.
	PresenceMinutes int `toml:"presence_minutes"`
	// Images is "auto" (inline images when the terminal can draw them) or "off".
	Images       string `toml:"images"`
	ImageMaxCols int    `toml:"image_max_cols"`
	ImageMaxRows int    `toml:"image_max_rows"`
	// ImageViewer opens images with `o`; other files use xdg-open.
	ImageViewer string `toml:"image_viewer"`
}

func Default() Config {
	return Config{TimeFormat: "15:04", SidebarWidth: 30, RefreshMinutes: 5, PresenceMinutes: 3, Images: "auto", ImageMaxCols: 40, ImageMaxRows: 12}
}

// Dir is the config directory.
func Dir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "slack-tui")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "slack-tui")
}

// Path is the config file location.
func Path() string { return filepath.Join(Dir(), "config.toml") }

// Load reads the config file. A missing file gives the defaults.
func Load() (Config, error) {
	c := Default()
	// loupe (GNOME's viewer) when installed: xdg-open sends images to the
	// browser on this machine.
	if _, err := exec.LookPath("loupe"); err == nil {
		c.ImageViewer = "loupe"
	}
	_, err := toml.DecodeFile(Path(), &c)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	d := Default()
	if c.TimeFormat == "" {
		c.TimeFormat = d.TimeFormat
	}
	if c.SidebarWidth < 16 {
		c.SidebarWidth = d.SidebarWidth
	}
	if c.RefreshMinutes <= 0 {
		c.RefreshMinutes = d.RefreshMinutes
	}
	if c.PresenceMinutes == 0 {
		c.PresenceMinutes = d.PresenceMinutes
	}
	if c.Images == "" {
		c.Images = d.Images
	}
	if c.ImageMaxCols <= 0 {
		c.ImageMaxCols = d.ImageMaxCols
	}
	if c.ImageMaxRows <= 0 {
		c.ImageMaxRows = d.ImageMaxRows
	}
	return c, nil
}
