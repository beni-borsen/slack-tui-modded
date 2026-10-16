package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/magnusdalkvist/slack-tui/internal/auth"
	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// quickSend handles `slack @person message…` and `slack '#channel' message…`.
// With no message words, the message is read from stdin (`echo hi | slack @anna`).
func quickSend(target string, words []string, dry bool) error {
	text := strings.Join(words, " ")
	if text == "" {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("no message — usage: slack @person 'message' (or pipe the message in)")
		}
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		text = strings.TrimRight(string(b), "\n")
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("empty message, nothing sent")
	}

	tokens, err := auth.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := backend.New(tokens)

	convID, where, err := resolveTarget(ctx, c, target, dry)
	if err != nil {
		return err
	}
	if dry {
		fmt.Printf("would send to %s: %q\n", where, render.Encode(text, nil))
		return nil
	}
	if _, err := c.Post(ctx, convID, render.Encode(text, nil), ""); err != nil {
		return err
	}
	fmt.Printf("sent to %s\n", where)
	return nil
}

// resolveTarget turns "@name", "#channel" or a bare name into a
// conversation. A bare name is tried as a channel first, then a person.
func resolveTarget(ctx context.Context, c *backend.Client, target string, dry bool) (id, where string, err error) {
	switch {
	case strings.HasPrefix(target, "#"):
		return findChannel(ctx, c, target[1:])
	case strings.HasPrefix(target, "@"):
		return findPerson(ctx, c, target[1:], dry)
	}
	id, where, err = findChannel(ctx, c, target)
	if err == nil {
		return id, where, nil
	}
	if id2, where2, err2 := findPerson(ctx, c, target, dry); err2 == nil {
		return id2, where2, nil
	} else if !errors.Is(err2, errNotFound) {
		return "", "", err2
	}
	return "", "", fmt.Errorf("no channel or person called %q", target)
}

// notFound is a target that doesn't exist, as opposed to an API failure.
type notFound struct{ msg string }

func (e notFound) Error() string { return e.msg }

var errNotFound = notFound{}

func (notFound) Is(target error) bool { _, ok := target.(notFound); return ok }

func findChannel(ctx context.Context, c *backend.Client, name string) (string, string, error) {
	convs, err := c.Conversations(ctx)
	if err != nil {
		return "", "", err
	}
	name = strings.ToLower(name)
	for _, cv := range convs {
		if !cv.IsDM() && strings.ToLower(cv.Name) == name {
			return cv.ID, "#" + cv.Name, nil
		}
	}
	return "", "", notFound{"you are not in a channel called #" + name}
}

// findPerson resolves a name to a DM, listing the candidates instead of
// guessing when the name is ambiguous. With dry set it doesn't open the DM
// (opening one can bring it back into the Slack sidebar).
func findPerson(ctx context.Context, c *backend.Client, name string, dry bool) (string, string, error) {
	users, err := c.Users(ctx)
	if err != nil {
		return "", "", err
	}
	matches := matchPerson(users, name)
	switch len(matches) {
	case 0:
		return "", "", notFound{"nobody called @" + name}
	case 1:
		u := matches[0]
		where := "@" + u.Label() + " (" + u.RealName + ")"
		if dry {
			return "", where, nil
		}
		id, err := c.OpenDM(ctx, u.ID)
		if err != nil {
			return "", "", err
		}
		return id, where, nil
	}
	var names []string
	for _, u := range matches {
		names = append(names, fmt.Sprintf("  @%s (%s, @%s)", u.Label(), u.RealName, u.Name))
	}
	slices.Sort(names)
	return "", "", fmt.Errorf("@%s matches %d people, be more specific:\n%s", name, len(matches), strings.Join(names, "\n"))
}

// matchPerson finds who a name means: exact matches on display name, full
// name or username win; otherwise everyone with a word starting with it.
func matchPerson(users []backend.User, name string) []backend.User {
	q := strings.ToLower(name)
	var exact, partial []backend.User
	for _, u := range users {
		if u.Deleted {
			continue
		}
		names := []string{u.DisplayName, u.RealName, u.Name}
		isExact, isPartial := false, false
		for _, n := range names {
			n = strings.ToLower(n)
			isExact = isExact || (n != "" && n == q)
			isPartial = isPartial || wordPrefix(n, q)
		}
		switch {
		case isExact:
			exact = append(exact, u)
		case isPartial:
			partial = append(partial, u)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}

// wordPrefix reports whether any word of s starts with q.
func wordPrefix(s, q string) bool {
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '.' || r == '_' || r == '-' }) {
		if strings.HasPrefix(w, q) {
			return true
		}
	}
	return false
}
