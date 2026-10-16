// Package auth stores the Slack tokens in the system keyring.
package auth

import (
	"errors"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"
)

const service = "slack-tui"

const (
	keyUser = "user-token" // xoxp-…, the user OAuth token
	keyApp  = "app-token"  // xapp-…, the app-level token for Socket Mode
)

// Tokens are the two credentials the client needs.
type Tokens struct {
	User string
	App  string
}

// ErrNoTokens means nothing is stored yet; run `slack-tui login`.
var ErrNoTokens = errors.New("no tokens stored — run `slack-tui login` first")

// Load reads the tokens. SLACK_USER_TOKEN / SLACK_APP_TOKEN override the
// keyring, which is handy for testing.
func Load() (Tokens, error) {
	t := Tokens{User: os.Getenv("SLACK_USER_TOKEN"), App: os.Getenv("SLACK_APP_TOKEN")}
	var err error
	if t.User == "" {
		if t.User, err = get(keyUser); err != nil {
			return t, err
		}
	}
	if t.App == "" {
		if t.App, err = get(keyApp); err != nil {
			return t, err
		}
	}
	return t, nil
}

func get(key string) (string, error) {
	v, err := keyring.Get(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNoTokens
	}
	if err != nil {
		return "", fmt.Errorf("reading %s from keyring: %w", key, err)
	}
	return v, nil
}

// Save writes both tokens to the keyring.
func Save(t Tokens) error {
	if err := keyring.Set(service, keyUser, t.User); err != nil {
		return fmt.Errorf("saving user token: %w", err)
	}
	if err := keyring.Set(service, keyApp, t.App); err != nil {
		return fmt.Errorf("saving app token: %w", err)
	}
	return nil
}

// Delete removes both tokens. Missing entries are not an error.
func Delete() error {
	for _, k := range []string{keyUser, keyApp} {
		if err := keyring.Delete(service, k); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return err
		}
	}
	return nil
}
