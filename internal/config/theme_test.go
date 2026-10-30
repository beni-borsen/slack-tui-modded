package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTheme(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if body == "" {
		return
	}
	os.MkdirAll(filepath.Join(dir, "slack-tui"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "slack-tui", "theme.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadThemeMissing(t *testing.T) {
	writeTheme(t, "")
	th, err := LoadTheme()
	if err != nil || !reflect.DeepEqual(th, DefaultTheme()) {
		t.Fatalf("got %+v, %v", th, err)
	}
}

func TestLoadThemePartial(t *testing.T) {
	writeTheme(t, "accent = \"#89b4fa\"\nnames = [\"#f38ba8\", \"117\"]\n")
	th, err := LoadTheme()
	if err != nil {
		t.Fatal(err)
	}
	if th.Accent != "#89b4fa" || len(th.Names) != 2 || th.Fg != DefaultTheme().Fg {
		t.Fatalf("got %+v", th)
	}
}

func TestLoadThemeBad(t *testing.T) {
	for body, want := range map[string]string{
		"accent = \"blue\"\n": "accent",
		"names = [\"#12\"]\n": "names",
		"acent = \"75\"\n":    "unknown key",
		"accent = \"256\"\n":  "accent",
	} {
		writeTheme(t, body)
		if _, err := LoadTheme(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want error with %q", body, err, want)
		}
	}
}

func TestLoadThemeAlertFollowsMention(t *testing.T) {
	writeTheme(t, "mention = \"#f38ba8\"\n")
	th, err := LoadTheme()
	if err != nil || th.Alert != "#f38ba8" {
		t.Fatalf("alert = %q, %v; want the theme's mention colour", th.Alert, err)
	}
	writeTheme(t, "mention = \"#f38ba8\"\nalert = \"196\"\n")
	if th, _ := LoadTheme(); th.Alert != "196" {
		t.Fatalf("alert = %q, want 196", th.Alert)
	}
}
