package userint

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"typefast/services/user/internal/retention"
)

func TestRetentionSweepDeletesDeadRows(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	user := h.registerAndVerify(t, "retention@example.com", "Sup3rSecret!pass")
	userID := uuid.MustParse(user.ID)

	insert := func(query string, args ...any) {
		t.Helper()
		if _, err := h.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}

	expiredToken := uuid.Must(uuid.NewV7())
	liveToken := uuid.Must(uuid.NewV7())
	insert(`INSERT INTO one_time_tokens (id, user_id, purpose, token_hash, expires_at)
		VALUES ($1, $2, 'email_verify', $3, now() - interval '1 hour')`, expiredToken, userID, []byte{1})
	insert(`INSERT INTO one_time_tokens (id, user_id, purpose, token_hash, expires_at)
		VALUES ($1, $2, 'password_reset', $3, now() + interval '1 hour')`, liveToken, userID, []byte{2})

	family := uuid.Must(uuid.NewV7())
	expiredSession := uuid.Must(uuid.NewV7())
	revokedSession := uuid.Must(uuid.NewV7())
	liveSession := uuid.Must(uuid.NewV7())
	insert(`INSERT INTO sessions (id, user_id, family_id, refresh_token_hash, expires_at)
		VALUES ($1, $2, $3, $4, now() - interval '1 hour')`, expiredSession, userID, family, []byte{3})
	insert(`INSERT INTO sessions (id, user_id, family_id, refresh_token_hash, expires_at, revoked_at)
		VALUES ($1, $2, $3, $4, now() + interval '1 hour', now())`, revokedSession, userID, family, []byte{4})
	insert(`INSERT INTO sessions (id, user_id, family_id, refresh_token_hash, expires_at)
		VALUES ($1, $2, $3, $4, now() + interval '1 hour')`, liveSession, userID, family, []byte{5})

	sentLongAgo := uuid.Must(uuid.NewV7())
	sentToday := uuid.Must(uuid.NewV7())
	pending := uuid.Must(uuid.NewV7())
	insert(`INSERT INTO outbox_events (id, topic, payload, attempts, processed_at, created_at)
		VALUES ($1, 'email.send', '{}', 1, now() - interval '30 days', now() - interval '31 days')`, sentLongAgo)
	insert(`INSERT INTO outbox_events (id, topic, payload, attempts, processed_at, created_at)
		VALUES ($1, 'email.send', '{}', 1, now(), now() - interval '1 day')`, sentToday)
	insert(`INSERT INTO outbox_events (id, topic, payload, attempts, created_at)
		VALUES ($1, 'email.send', '{}', 0, now() - interval '30 days')`, pending)

	sweeper := &retention.Sweeper{
		Store:        h.store,
		Logger:       h.logger,
		OutboxWindow: 7 * 24 * time.Hour,
	}
	result, err := sweeper.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if result.OneTimeTokens != 1 || result.Sessions != 1 || result.OutboxEvents != 1 {
		t.Fatalf("sweep result = %+v, want one row from each table", result)
	}

	exists := func(table string, id uuid.UUID) bool {
		t.Helper()
		var found bool
		if err := h.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id = $1)`, id).Scan(&found); err != nil {
			t.Fatal(err)
		}
		return found
	}

	if exists("one_time_tokens", expiredToken) {
		t.Fatal("the expired token should have been deleted")
	}
	if !exists("one_time_tokens", liveToken) {
		t.Fatal("an unexpired token must survive")
	}

	if exists("sessions", expiredSession) {
		t.Fatal("the expired session should have been deleted")
	}
	if !exists("sessions", liveSession) {
		t.Fatal("an unexpired session must survive")
	}
	if !exists("sessions", revokedSession) {
		t.Fatal("a revoked but unexpired session must stay visible until it expires")
	}

	if exists("outbox_events", sentLongAgo) {
		t.Fatal("a delivered outbox row past the window should have been deleted")
	}
	if !exists("outbox_events", sentToday) {
		t.Fatal("a recently delivered outbox row must stay inside the window")
	}
	if !exists("outbox_events", pending) {
		t.Fatal("an undelivered outbox row must never be deleted")
	}
}

// A sweep stops after a bounded number of batches, so the next tick has to pick
// up whatever is left instead of leaving the backlog in place forever.
func TestRetentionSweepDrainsLargeBacklog(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	user := h.registerAndVerify(t, "backlog@example.com", "Sup3rSecret!pass")
	userID := uuid.MustParse(user.ID)

	if _, err := h.pool.Exec(ctx, `
		INSERT INTO one_time_tokens (id, user_id, purpose, token_hash, expires_at)
		SELECT gen_random_uuid(), $1, 'email_verify', decode(md5(i::text), 'hex'), now() - interval '1 hour'
		FROM generate_series(1, 1100) AS i
	`, userID); err != nil {
		t.Fatal(err)
	}

	sweeper := &retention.Sweeper{Store: h.store, Logger: h.logger, OutboxWindow: 7 * 24 * time.Hour}
	result, err := sweeper.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Every expired row, spread over two batches of 1000.
	if result.OneTimeTokens != 1100 {
		t.Fatalf("deleted %d tokens, want 1100 across both batches", result.OneTimeTokens)
	}

	var remaining int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM one_time_tokens WHERE expires_at < now()`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("%d expired tokens left behind", remaining)
	}

	// A consumed token is kept until it expires so a replay is still reported as
	// already used rather than as an unknown token.
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM one_time_tokens WHERE expires_at > now()`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("unexpired tokens = %d, want the consumed registration token", remaining)
	}
}
