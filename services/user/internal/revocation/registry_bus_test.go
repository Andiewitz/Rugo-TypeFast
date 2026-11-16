package revocation_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"typefast/services/user/internal/revocation"
)

// recordingPublisher captures what the registry broadcasts.
type recordingPublisher struct {
	mu     sync.Mutex
	events []revocation.Event
	err    error
}

func (p *recordingPublisher) Publish(_ context.Context, event revocation.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
	return p.err
}

func (p *recordingPublisher) only(t *testing.T) revocation.Event {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.events) != 1 {
		t.Fatalf("published %d events, want 1", len(p.events))
	}
	return p.events[0]
}

func TestRevokePublishesSuspension(t *testing.T) {
	userID := uuid.New()
	banAt := time.Now().UTC()
	publisher := &recordingPublisher{}

	registry := revocation.NewRegistry(&fakeSource{}, nil)
	registry.SetPublisher(publisher)
	registry.Revoke(userID, banAt)

	event := publisher.only(t)
	if event.UserID != userID || !event.Suspended || !event.Before.Equal(banAt) {
		t.Fatalf("event = %+v", event)
	}
}

func TestMarkIssuedBeforePublishesCutoff(t *testing.T) {
	userID := uuid.New()
	cutoff := time.Now().UTC()
	publisher := &recordingPublisher{}

	registry := revocation.NewRegistry(&fakeSource{}, nil)
	registry.SetPublisher(publisher)
	registry.MarkIssuedBefore(userID, cutoff)

	event := publisher.only(t)
	if event.Suspended {
		t.Fatal("a password reset deadline must not suspend the account")
	}
	if !event.Before.Equal(cutoff) {
		t.Fatalf("event.Before = %v, want %v", event.Before, cutoff)
	}
}

func TestRestorePublishesLift(t *testing.T) {
	userID := uuid.New()
	banAt := time.Now().UTC()
	publisher := &recordingPublisher{}

	registry := revocation.NewRegistry(&fakeSource{}, nil)
	registry.SetPublisher(publisher)
	registry.Restore(userID, banAt)

	event := publisher.only(t)
	if event.Suspended {
		t.Fatal("restore must publish a lifted suspension")
	}
	if !event.Before.Equal(banAt) {
		t.Fatalf("restore must keep the marker, got %v", event.Before)
	}
}

// A bus that is down must never undo a ban locally.
func TestPublishFailureKeepsLocalState(t *testing.T) {
	userID := uuid.New()
	banAt := time.Now().UTC()

	registry := revocation.NewRegistry(&fakeSource{}, nil)
	registry.SetPublisher(&recordingPublisher{err: errors.New("redis down")})
	registry.Revoke(userID, banAt)

	if !registry.TokenRevoked(userID, banAt.Add(time.Hour)) {
		t.Fatal("ban must apply even when the broadcast fails")
	}
}

func TestRegistryWithoutPublisherStaysLocal(t *testing.T) {
	userID := uuid.New()
	registry := revocation.NewRegistry(&fakeSource{}, nil)

	registry.Revoke(userID, time.Now().UTC())

	if !registry.TokenRevoked(userID, time.Now().UTC()) {
		t.Fatal("local bans must work without a publisher")
	}
}

func TestApplyAcceptsNewerEvent(t *testing.T) {
	userID := uuid.New()
	first := time.Now().UTC().Add(-time.Hour)
	second := time.Now().UTC()

	registry := revocation.NewRegistry(&fakeSource{}, nil)
	registry.Apply(revocation.Event{UserID: userID, Before: first, Suspended: true})
	registry.Apply(revocation.Event{UserID: userID, Before: second})

	if registry.TokenRevoked(userID, second.Add(time.Minute)) {
		t.Fatal("the newer event lifted the suspension; fresh tokens must work")
	}
	if !registry.TokenRevoked(userID, second.Add(-time.Minute)) {
		t.Fatal("tokens issued before the newer marker must stay refused")
	}
	if !registry.TokenRevoked(userID, first.Add(-time.Minute)) {
		t.Fatal("tokens issued before the original ban must stay refused")
	}
}

func TestApplyIgnoresOlderEvent(t *testing.T) {
	userID := uuid.New()
	newer := time.Now().UTC()
	older := newer.Add(-time.Hour)

	registry := revocation.NewRegistry(&fakeSource{}, nil)
	registry.Apply(revocation.Event{UserID: userID, Before: newer})
	registry.Apply(revocation.Event{UserID: userID, Before: older, Suspended: true})

	if registry.TokenRevoked(userID, newer) {
		t.Fatal("an older ban must not override a newer record")
	}
}

// Two replicas can receive a ban and a lift with the same marker, because a
// reactivation keeps the marker. Refusing is the safe answer until the refresh.
func TestApplyKeepsSuspensionOnTie(t *testing.T) {
	userID := uuid.New()
	banAt := time.Now().UTC()

	registry := revocation.NewRegistry(&fakeSource{}, nil)
	registry.Apply(revocation.Event{UserID: userID, Before: banAt, Suspended: true})
	registry.Apply(revocation.Event{UserID: userID, Before: banAt})

	if !registry.TokenRevoked(userID, banAt.Add(time.Hour)) {
		t.Fatal("a tie must keep the suspension")
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	userID := uuid.New()
	banAt := time.Now().UTC()
	event := revocation.Event{UserID: userID, Before: banAt, Suspended: true}

	registry := revocation.NewRegistry(&fakeSource{}, nil)
	registry.Apply(event)
	registry.Apply(event)
	registry.Apply(event)

	if !registry.TokenRevoked(userID, banAt.Add(time.Hour)) {
		t.Fatal("repeated events must leave the suspension in place")
	}
	if registry.TokenRevoked(uuid.New(), banAt.Add(time.Hour)) {
		t.Fatal("a repeated event must not touch other accounts")
	}
}

func TestApplyAddsUnknownAccount(t *testing.T) {
	userID := uuid.New()
	banAt := time.Now().UTC()

	registry := revocation.NewRegistry(&fakeSource{}, nil)
	registry.Apply(revocation.Event{UserID: userID, Before: banAt, Suspended: true})

	if !registry.TokenRevoked(userID, banAt.Add(time.Hour)) {
		t.Fatal("an event for an unseen account must be applied")
	}
}

// Events arriving while requests are being checked must not race.
func TestApplyAndReadConcurrently(t *testing.T) {
	userID := uuid.New()
	banAt := time.Now().UTC()
	registry := revocation.NewRegistry(&fakeSource{}, nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			registry.Apply(revocation.Event{UserID: userID, Before: banAt, Suspended: i%2 == 0})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = registry.TokenRevoked(userID, banAt)
		}
	}()
	wg.Wait()

	if !registry.TokenRevoked(userID, banAt.Add(time.Hour)) {
		t.Fatal("after the goroutines settle a ban must still win the tie")
	}
}
