// Package names keeps a small local list of people and channels for shell
// tab completion, which must answer instantly and can't call Slack.
package names

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

type Person struct {
	ID          string `json:"id"`
	Name        string `json:"name"` // username
	DisplayName string `json:"display_name,omitempty"`
	RealName    string `json:"real_name,omitempty"`
}

type Cache struct {
	Updated  time.Time `json:"updated"`
	People   []Person  `json:"people"`
	Channels []string  `json:"channels"`
}

// MaxAge is when completion starts a background refresh.
const MaxAge = 24 * time.Hour

// Path is where the cache lives.
func Path() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "slack-tui", "names.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "slack-tui", "names.json")
}

// Save writes the people (not deactivated ones) and the channels we are in.
func Save(users []backend.User, convs []backend.Conversation) error {
	return SaveTo(Path(), users, convs)
}

// SaveTo is Save to a given file. Callers saving in the background resolve
// the path first, so the file can't change under them.
func SaveTo(p string, users []backend.User, convs []backend.Conversation) error {
	c := Cache{Updated: time.Now()}
	for _, u := range users {
		if !u.Deleted {
			c.People = append(c.People, Person{ID: u.ID, Name: u.Name, DisplayName: u.DisplayName, RealName: u.RealName})
		}
	}
	for _, cv := range convs {
		if !cv.IsDM() {
			c.Channels = append(c.Channels, cv.Name)
		}
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	// Write then rename, so a completion reading at the same moment never
	// sees half a file.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Load reads the cache; ok is false when there is none.
func Load() (Cache, bool) {
	var c Cache
	b, err := os.ReadFile(Path())
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, false
	}
	return c, true
}
