// Package revocation decides whether an access token has been invalidated by a
// ban or a forced sign-out.
//
// Access tokens are signed and verified offline, so nothing can recall one
// after it is issued. Instead the service keeps a small in-memory snapshot of
// revoked accounts, refreshed from storage on a timer.
//
// An account is rejected in one of two ways:
//   - it is suspended, so every token is refused regardless of age;
//   - it carries a marker from an earlier ban, so tokens issued at or before
//     that marker stay refused even after the account is reactivated.
//
// The snapshot holds revoked accounts only, so its size follows the number of
// bans rather than the number of users, and the per-request cost is a map
// lookup with no database round trip.
package revocation

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Entry is one account's revocation state.
type Entry struct {
	UserID uuid.UUID

	// Before is the moment the account was last banned. Tokens issued at or
	// before it are refused forever, even after reactivation.
	Before time.Time

	// Suspended refuses every token, including ones minted after the ban. This
	// is what makes a ban absolute: a refresh through an external identity
	// provider cannot mint its way back in.
	Suspended bool

	// SentAt orders changes that share a marker. A ban and the reactivation
	// that lifts it carry the same Before, so without this a replica could not
	// tell the ban from the later lift. Zero means unknown.
	SentAt time.Time
}

// Event is one change to the revocation set. It carries the state another
// replica should hold, never an instruction, so applying it twice is harmless.
type Event struct {
	UserID    uuid.UUID `json:"user_id"`
	Before    time.Time `json:"before"`
	Suspended bool      `json:"suspended"`
	SentAt    time.Time `json:"sent_at,omitempty"`
}

// Publisher broadcasts a change to other replicas. A nil Publisher keeps the
// registry local, which is what a single-instance deployment needs.
type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

// Source loads the current set of revoked accounts.
type Source interface {
	ListRevocations(ctx context.Context) ([]Entry, error)
}

// Registry is a concurrency-safe snapshot of revoked accounts.
type Registry struct {
	mu        sync.RWMutex
	revoked   map[uuid.UUID]Entry
	source    Source
	publisher Publisher
	logger    *slog.Logger
}

// NewRegistry creates an empty registry. Call Refresh before serving traffic so
// bans recorded before startup are enforced.
func NewRegistry(source Source, logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		source:  source,
		logger:  logger,
		revoked: make(map[uuid.UUID]Entry),
	}
}

// Refresh replaces the snapshot with the current state from storage. When the
// source fails the previous snapshot is kept, so a database hiccup cannot
// silently un-ban everyone; the error is returned for the caller to log.
func (r *Registry) Refresh(ctx context.Context) error {
	entries, err := r.source.ListRevocations(ctx)
	if err != nil {
		return fmt.Errorf("revocation: refresh: %w", err)
	}

	next := make(map[uuid.UUID]Entry, len(entries))
	for _, entry := range entries {
		next[entry.UserID] = entry
	}

	r.mu.Lock()
	r.revoked = next
	r.mu.Unlock()
	return nil
}

// Revoke records a ban locally so the replica that wrote it enforces the ban on
// the next request instead of waiting for the next refresh.
func (r *Registry) Revoke(userID uuid.UUID, before time.Time) {
	r.mu.Lock()
	r.revoked[userID] = Entry{UserID: userID, Before: before, Suspended: true, SentAt: time.Now().UTC()}
	r.mu.Unlock()

	r.publish(Event{UserID: userID, Before: before, Suspended: true})
}

// MarkIssuedBefore records a cut-off locally without suspending the account, so
// tokens issued at or before before are refused while fresh ones are accepted.
// It is used when a password reset forces every existing session out.
func (r *Registry) MarkIssuedBefore(userID uuid.UUID, before time.Time) {
	if before.IsZero() {
		return
	}
	r.mu.Lock()
	r.revoked[userID] = Entry{UserID: userID, Before: before, SentAt: time.Now().UTC()}
	r.mu.Unlock()

	r.publish(Event{UserID: userID, Before: before})
}

// Restore lifts a suspension locally, keeping the marker so tokens issued at or
// before it stay refused. An account that was never banned is dropped from the
// snapshot entirely.
func (r *Registry) Restore(userID uuid.UUID, before time.Time) {
	r.mu.Lock()
	if before.IsZero() {
		delete(r.revoked, userID)
	} else {
		r.revoked[userID] = Entry{UserID: userID, Before: before, SentAt: time.Now().UTC()}
	}
	r.mu.Unlock()

	r.publish(Event{UserID: userID, Before: before})
}

// SetPublisher attaches a broadcast channel. Call it before serving traffic.
// A failed publish never fails a ban: storage is the source of truth, and the
// periodic refresh corrects any replica that missed the message.
func (r *Registry) SetPublisher(publisher Publisher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publisher = publisher
}

// Apply merges an event received from another replica.
//
// Newer markers replace older ones and an older event is ignored. A ban and the
// reactivation that lifts it share a marker, so SentAt decides between them; a
// lift that is not provably newer is dropped, because refusing a live session
// is recoverable and letting a banned one through is not.
func (r *Registry) Apply(event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if current, ok := r.revoked[event.UserID]; ok {
		if event.Before.Before(current.Before) {
			return
		}
		if event.Before.Equal(current.Before) {
			if event.SentAt.Before(current.SentAt) {
				return
			}
			if event.SentAt.Equal(current.SentAt) {
				// Same marker at the same instant: keep the stricter state, so a
				// payload without a send time can never lift a suspension.
				if current.Suspended || !event.Suspended {
					return
				}
			}
		}
	}

	r.revoked[event.UserID] = Entry{
		UserID:    event.UserID,
		Before:    event.Before,
		Suspended: event.Suspended,
		SentAt:    event.SentAt,
	}
}

const publishTimeout = 250 * time.Millisecond

func (r *Registry) publish(event Event) {
	r.mu.RLock()
	publisher := r.publisher
	r.mu.RUnlock()
	if publisher == nil {
		return
	}

	if event.SentAt.IsZero() {
		event.SentAt = time.Now().UTC()
	}

	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()
	if err := publisher.Publish(ctx, event); err != nil {
		r.logger.Warn("revocation broadcast failed", "user_id", event.UserID, "error", err)
	}
}

// TokenRevoked reports whether a token issued at issuedAt must be rejected.
// A zero issuedAt is treated as revoked: without a timestamp there is no way to
// prove the token postdates the ban.
func (r *Registry) TokenRevoked(userID uuid.UUID, issuedAt time.Time) bool {
	r.mu.RLock()
	entry, ok := r.revoked[userID]
	r.mu.RUnlock()
	if !ok {
		return false
	}

	if entry.Suspended {
		return true
	}
	if issuedAt.IsZero() {
		return true
	}
	// JWT issued-at is truncated to whole seconds when the token is signed, so
	// a token minted just after a ban can still carry an issued-at inside the
	// ban's own second. Comparing against the truncated marker allows that
	// sub-second window instead of rejecting legitimate fresh tokens.
	return issuedAt.Before(entry.Before.Truncate(time.Second))
}

// Run refreshes the snapshot on every tick until ctx is cancelled. Refresh
// failures are logged and the previous snapshot keeps being enforced.
func (r *Registry) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Refresh(ctx); err != nil {
				r.logger.Warn("token revocation refresh failed", "error", err)
			}
		}
	}
}
