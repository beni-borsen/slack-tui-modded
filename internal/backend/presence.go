package backend

import (
	"context"
	"fmt"
	"time"

	"github.com/slack-go/slack"
)

// DND is the user's Do Not Disturb state: scheduled quiet hours and/or a
// snooze started by hand.
type DND struct {
	// Start and End are the current or next scheduled window; zero when
	// the user has no DND hours.
	Start, End time.Time
	SnoozeEnd  time.Time // zero unless snoozed
}

func newDND(enabled bool, start, end int64, snoozed bool, snoozeEnd int64) DND {
	var d DND
	if enabled && start > 0 && end > 0 {
		d.Start, d.End = time.Unix(start, 0), time.Unix(end, 0)
	}
	if snoozed && snoozeEnd > 0 {
		d.SnoozeEnd = time.Unix(snoozeEnd, 0)
	}
	return d
}

// Snoozed is true while a snooze started by hand is running.
func (d DND) Snoozed(now time.Time) bool { return now.Before(d.SnoozeEnd) }

// Scheduled is true inside the scheduled quiet hours.
func (d DND) Scheduled(now time.Time) bool {
	return !d.Start.IsZero() && !now.Before(d.Start) && now.Before(d.End)
}

// Until reports whether notifications are paused at now, and until when.
func (d DND) Until(now time.Time) (time.Time, bool) {
	var until time.Time
	if d.Snoozed(now) {
		until = d.SnoozeEnd
	}
	if d.Scheduled(now) && d.End.After(until) {
		until = d.End
	}
	return until, !until.IsZero()
}

// Next is the next moment the state changes on its own, or zero.
func (d DND) Next(now time.Time) time.Time {
	var next time.Time
	for _, t := range []time.Time{d.SnoozeEnd, d.Start, d.End} {
		if t.After(now) && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	return next
}

func fromDNDStatus(s *slack.DNDStatus) DND {
	return newDND(s.Enabled, int64(s.NextStartTimestamp), int64(s.NextEndTimestamp), s.SnoozeEnabled, int64(s.SnoozeEndTime))
}

// DNDInfo returns the user's own Do Not Disturb state.
func (c *Client) DNDInfo(ctx context.Context) (DND, error) {
	var s *slack.DNDStatus
	err := retry(ctx, func() (err error) {
		s, err = c.api.GetDNDInfoContext(ctx, nil)
		return err
	})
	if err != nil {
		return DND{}, fmt.Errorf("dnd.info: %w", err)
	}
	return fromDNDStatus(s), nil
}

// Snooze pauses notifications for the given minutes (replacing a running
// snooze) and returns the new state.
func (c *Client) Snooze(ctx context.Context, minutes int) (DND, error) {
	err := retry(ctx, func() error {
		_, err := c.api.SetSnoozeContext(ctx, minutes)
		return err
	})
	if err != nil {
		return DND{}, fmt.Errorf("dnd.setSnooze: %w", err)
	}
	// The answer only describes the snooze; the schedule needs dnd.info.
	return c.DNDInfo(ctx)
}

// EndDND turns notifications back on: it ends a snooze and, inside the
// scheduled hours, the current window.
func (c *Client) EndDND(ctx context.Context, d DND) (DND, error) {
	now := time.Now()
	if d.Snoozed(now) {
		err := retry(ctx, func() error {
			_, err := c.api.EndSnoozeContext(ctx)
			return err
		})
		if err != nil {
			return DND{}, fmt.Errorf("dnd.endSnooze: %w", err)
		}
	}
	if d.Scheduled(now) {
		if err := retry(ctx, func() error { return c.api.EndDNDContext(ctx) }); err != nil {
			return DND{}, fmt.Errorf("dnd.endDnd: %w", err)
		}
	}
	return c.DNDInfo(ctx)
}

// Presence is someone's presence: active, or away (by hand when it is
// the user's own and they chose it).
type Presence struct {
	Active     bool
	ManualAway bool // only reported for the user themselves
}

func (c *Client) Presence(ctx context.Context, userID string) (Presence, error) {
	var p *slack.UserPresence
	err := retry(ctx, func() (err error) {
		p, err = c.api.GetUserPresenceContext(ctx, userID)
		return err
	})
	if err != nil {
		return Presence{}, fmt.Errorf("users.getPresence: %w", err)
	}
	return Presence{Active: p.Presence == "active", ManualAway: p.ManualAway}, nil
}

// SetAway marks the user away, or with away=false lets Slack decide again.
func (c *Client) SetAway(ctx context.Context, away bool) error {
	presence := "auto"
	if away {
		presence = "away"
	}
	if err := retry(ctx, func() error { return c.api.SetUserPresenceContext(ctx, presence) }); err != nil {
		return fmt.Errorf("users.setPresence: %w", err)
	}
	return nil
}

// SetStatus sets the user's custom status; empty emoji and text clear it.
// A zero expires keeps it until changed.
func (c *Client) SetStatus(ctx context.Context, emoji, text string, expires time.Time) error {
	var exp int64
	if !expires.IsZero() {
		exp = expires.Unix()
	}
	err := retry(ctx, func() error { return c.api.SetUserCustomStatusContext(ctx, text, emoji, exp) })
	if err != nil {
		return fmt.Errorf("users.profile.set: %w", err)
	}
	return nil
}
