package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"

	"github.com/BurntSushi/toml"
)

// Theme is the color palette, read from ~/.config/slack-tui/theme.toml. It is
// its own file so it can live in a dotfiles repo apart from config.toml's
// workspace-specific sections. A color is an ANSI 256 number ("75") or hex
// ("#89b4fa", "#fff"); missing keys keep the default.
type Theme struct {
	Fg             string `toml:"fg"`               // normal text
	Dim            string `toml:"dim"`              // timestamps, headers, topics, quotes
	Faint          string `toml:"faint"`            // inactive borders
	Accent         string `toml:"accent"`           // links, mentions, threads, focused border, normal-mode pill
	Unread         string `toml:"unread"`           // unread conversations, titles
	Mention        string `toml:"mention"`          // mention badges, "new" line, errors
	Self           string `toml:"self"`             // background of mentions of you
	CodeBg         string `toml:"code_bg"`          // code, reactions
	SelectionBg    string `toml:"selection_bg"`     // cursor and active row
	OK             string `toml:"ok"`               // insert-mode pill, success
	Warn           string `toml:"warn"`             // warnings
	BadgeFg        string `toml:"badge_fg"`         // text on mention badges
	ContrastFg     string `toml:"contrast_fg"`      // text on mode pills and mentions of you
	ReactionSelfBg string `toml:"reaction_self_bg"` // reactions you added
	CodeFg         string `toml:"code_fg"`          // inline code
	CodeBlockFg    string `toml:"code_block_fg"`    // code blocks
	CustomEmoji    string `toml:"custom_emoji"`     // custom emoji shown as :name:
	OwnName        string `toml:"own_name"`         // "you" on your own messages
	OwnText        string `toml:"own_text"`         // the text of your own messages
	OtherText      string `toml:"other_text"`       // the text of everyone else's messages
	UnreadsBadge   string `toml:"unreads_badge"`    // the total on the Unreads row
	Alert          string `toml:"alert"`            // panes and pill on a new message in a watched channel (default: mention)
	// Names are the colors sender names are picked from.
	Names []string `toml:"names"`
}

func DefaultTheme() Theme {
	t := baseTheme()
	t.Alert = t.Mention
	return t
}

// baseTheme is the default with Alert left empty, so a theme that sets
// mention but not alert gets its own mention colour for alerts.
func baseTheme() Theme {
	return Theme{
		Fg:             "252",
		Dim:            "244",
		Faint:          "238",
		Accent:         "75",
		Unread:         "231",
		Mention:        "203",
		Self:           "221",
		CodeBg:         "236",
		SelectionBg:    "237",
		OK:             "114",
		Warn:           "215",
		BadgeFg:        "16",
		ContrastFg:     "16",
		ReactionSelfBg: "24",
		CodeFg:         "217",
		CodeBlockFg:    "252",
		CustomEmoji:    "180",
		OwnName:        "245",
		OwnText:        "250",
		OtherText:      "253",
		UnreadsBadge:   "209",
		Names:          []string{"75", "114", "176", "215", "80", "211", "150", "179", "141", "109"},
	}
}

// ThemePath is the theme file location.
func ThemePath() string { return filepath.Join(Dir(), "theme.toml") }

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

func validColor(s string) bool {
	if hexColor.MatchString(s) {
		return true
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0 && n <= 255
}

// LoadTheme reads the theme file. A missing file gives the default theme.
func LoadTheme() (Theme, error) {
	t := baseTheme()
	md, err := toml.DecodeFile(ThemePath(), &t)
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultTheme(), nil
	}
	if err != nil {
		return DefaultTheme(), err
	}
	if t.Alert == "" {
		t.Alert = t.Mention
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return DefaultTheme(), fmt.Errorf("unknown key %q", keys[0].String())
	}
	if len(t.Names) == 0 {
		t.Names = DefaultTheme().Names
	}
	v := reflect.ValueOf(t)
	for i := 0; i < v.NumField(); i++ {
		key := v.Type().Field(i).Tag.Get("toml")
		switch f := v.Field(i).Interface().(type) {
		case string:
			if !validColor(f) {
				return DefaultTheme(), fmt.Errorf("%s: bad color %q (want 0-255 or #rrggbb)", key, f)
			}
		case []string:
			for _, c := range f {
				if !validColor(c) {
					return DefaultTheme(), fmt.Errorf("%s: bad color %q (want 0-255 or #rrggbb)", key, c)
				}
			}
		}
	}
	return t, nil
}
