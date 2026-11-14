// Package retention deletes rows that can no longer be used: expired one-time
// tokens, expired sessions, and outbox events that were already delivered.
//
// Nothing ever reads those rows again, but each one was written by ordinary
// traffic — a token per registration or password reset, a session per sign-in,
// an outbox row per email — so left alone they grow without bound. The sweeper
// runs on a timer inside the API process, like the revocation refresher and the
// outbox worker, so a deployment needs no external scheduler.
package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	// DefaultInterval keeps the sweep off the request path while staying far
	// ahead of real growth.
	DefaultInterval = time.Hour

	// DefaultOutboxWindow keeps delivered messages around long enough to
	// investigate a missing email.
	DefaultOutboxWindow = 7 * 24 * time.Hour
)

// Store deletes rows older than the given moment and reports how many it
// removed.
type Store interface {
	DeleteExpiredOneTimeTokens(ctx context.Context, before time.Time) (int64, error)
	DeleteExpiredSessions(ctx context.Context, before time.Time) (int64, error)
	DeleteProcessedOutboxEvents(ctx context.Context, before time.Time) (int64, error)
}

// Result reports how many rows one sweep removed.
type Result struct {
	OneTimeTokens int64
	Sessions      int64
	OutboxEvents  int64
}

// Total is the number of rows the sweep deleted.
func (r Result) Total() int64 {
	return r.OneTimeTokens + r.Sessions + r.OutboxEvents
}

// Sweeper deletes dead rows on a timer.
//
// Tokens and sessions are removed the moment they expire: by then a token can
// no longer be redeemed and a session's refresh token can no longer be rotated.
// Delivered outbox rows are kept for OutboxWindow because operators inspect
// recent sends when an email does not arrive.
type Sweeper struct {
	Store        Store
	Logger       *slog.Logger
	Interval     time.Duration
	OutboxWindow time.Duration

	// Now is replaceable so tests can pin the clock; nil means time.Now.
	Now func() time.Time
}

// Run sweeps once immediately, then on every interval until ctx is cancelled.
// Sweeping at startup lets a replica that was down for a while catch up at once
// instead of waiting a whole interval.
func (s *Sweeper) Run(ctx context.Context) {
	interval := s.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}

	s.sweepLogged(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepLogged(ctx)
		}
	}
}

// Sweep deletes everything that is dead as of now. Every table is attempted even
// when an earlier delete fails, because one broken statement must not stop the
// rest of the cleanup; the failures are returned together.
func (s *Sweeper) Sweep(ctx context.Context) (Result, error) {
	now := s.now()

	window := s.OutboxWindow
	if window <= 0 {
		window = DefaultOutboxWindow
	}

	var result Result
	var failures []error

	deleted, err := s.Store.DeleteExpiredOneTimeTokens(ctx, now)
	if err != nil {
		failures = append(failures, fmt.Errorf("delete expired one-time tokens: %w", err))
	} else {
		result.OneTimeTokens = deleted
	}

	deleted, err = s.Store.DeleteExpiredSessions(ctx, now)
	if err != nil {
		failures = append(failures, fmt.Errorf("delete expired sessions: %w", err))
	} else {
		result.Sessions = deleted
	}

	deleted, err = s.Store.DeleteProcessedOutboxEvents(ctx, now.Add(-window))
	if err != nil {
		failures = append(failures, fmt.Errorf("delete delivered outbox events: %w", err))
	} else {
		result.OutboxEvents = deleted
	}

	if len(failures) > 0 {
		return result, errors.Join(failures...)
	}

	if result.Total() > 0 {
		s.logger().Info("retention sweep removed dead rows",
			"one_time_tokens", result.OneTimeTokens,
			"sessions", result.Sessions,
			"outbox_events", result.OutboxEvents,
		)
	}
	return result, nil
}

func (s *Sweeper) sweepLogged(ctx context.Context) {
	if _, err := s.Sweep(ctx); err != nil {
		// Partial deletions stay deleted, so this warns rather than fails the
		// process; the next tick retries the table that broke.
		s.logger().Warn("retention sweep failed", "error", err)
	}
}

func (s *Sweeper) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Sweeper) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
