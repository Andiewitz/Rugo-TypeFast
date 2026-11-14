package retention_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"typefast/services/user/internal/retention"
)

type fakeStore struct {
	mu sync.Mutex

	tokenCutoff   time.Time
	sessionCutoff time.Time
	outboxCutoff  time.Time

	tokens       int64
	sessions     int64
	outboxEvents int64

	tokenErr   error
	sessionErr error
	outboxErr  error

	sweeps int
}

func (f *fakeStore) DeleteExpiredOneTimeTokens(_ context.Context, before time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweeps++
	f.tokenCutoff = before
	return f.tokens, f.tokenErr
}

func (f *fakeStore) DeleteExpiredSessions(_ context.Context, before time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionCutoff = before
	return f.sessions, f.sessionErr
}

func (f *fakeStore) DeleteProcessedOutboxEvents(_ context.Context, before time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outboxCutoff = before
	return f.outboxEvents, f.outboxErr
}

func (f *fakeStore) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sweeps
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Tokens and sessions are dead when they expire, so both are cut at now; sent
// outbox rows are only cut after the inspection window.
func TestSweepCutoffs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{}
	sweeper := &retention.Sweeper{
		Store:        store,
		Logger:       discardLogger(),
		OutboxWindow: 24 * time.Hour,
		Now:          func() time.Time { return now },
	}

	if _, err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !store.tokenCutoff.Equal(now) {
		t.Fatalf("token cutoff = %s, want %s", store.tokenCutoff, now)
	}
	if !store.sessionCutoff.Equal(now) {
		t.Fatalf("session cutoff = %s, want %s", store.sessionCutoff, now)
	}
	if want := now.Add(-24 * time.Hour); !store.outboxCutoff.Equal(want) {
		t.Fatalf("outbox cutoff = %s, want %s", store.outboxCutoff, want)
	}
}

func TestSweepReportsCounts(t *testing.T) {
	t.Parallel()

	store := &fakeStore{tokens: 3, sessions: 2, outboxEvents: 5}
	sweeper := &retention.Sweeper{Store: store, Logger: discardLogger()}

	result, err := sweeper.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.OneTimeTokens != 3 || result.Sessions != 2 || result.OutboxEvents != 5 {
		t.Fatalf("result = %+v", result)
	}
	if result.Total() != 10 {
		t.Fatalf("total = %d, want 10", result.Total())
	}
}

// A default window keeps delivered mail around for a week.
func TestSweepDefaultsOutboxWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{}
	sweeper := &retention.Sweeper{
		Store:  store,
		Logger: discardLogger(),
		Now:    func() time.Time { return now },
	}

	if _, err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := now.Add(-retention.DefaultOutboxWindow); !store.outboxCutoff.Equal(want) {
		t.Fatalf("outbox cutoff = %s, want %s", store.outboxCutoff, want)
	}
}

// One broken delete must not stop the others, and the failure still surfaces.
func TestSweepContinuesAfterFailure(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		tokenErr:     errors.New("lock timeout"),
		sessions:     4,
		outboxEvents: 6,
	}
	sweeper := &retention.Sweeper{Store: store, Logger: discardLogger()}

	result, err := sweeper.Sweep(context.Background())
	if err == nil {
		t.Fatal("expected the token failure to surface")
	}
	if !strings.Contains(err.Error(), "one-time tokens") {
		t.Fatalf("error = %v", err)
	}
	if result.Sessions != 4 || result.OutboxEvents != 6 {
		t.Fatalf("other tables should still be swept, result = %+v", result)
	}
	if store.sessionCutoff.IsZero() || store.outboxCutoff.IsZero() {
		t.Fatal("sessions and outbox should still have been attempted")
	}
}

func TestRunSweepsImmediatelyAndThenOnTicks(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	sweeper := &retention.Sweeper{Store: store, Logger: discardLogger(), Interval: 5 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		sweeper.Run(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for store.calls() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	if got := store.calls(); got < 2 {
		t.Fatalf("sweeps = %d, want the startup sweep plus at least one tick", got)
	}
}

func TestRunDoesNotSweepAfterCancel(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	sweeper := &retention.Sweeper{Store: store, Logger: discardLogger(), Interval: time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sweeper.Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return for an already-cancelled context")
	}
}
