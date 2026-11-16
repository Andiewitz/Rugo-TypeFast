package postgres

import (
	"context"
	"time"
)

// A sweep deletes in bounded batches rather than one unbounded DELETE: a heavily
// neglected table would otherwise hold locks and grow WAL for a whole table at
// once. Each call stops after cleanupBatches passes and the next tick continues
// where this one stopped, so the backlog drains without a long stall.
const (
	cleanupBatchSize = 1000
	cleanupBatches   = 10
)

const (
	deleteExpiredTokens = `
		DELETE FROM one_time_tokens
		WHERE id IN (SELECT id FROM one_time_tokens WHERE expires_at < $1 LIMIT $2)
	`
	deleteExpiredSessions = `
		DELETE FROM sessions
		WHERE id IN (SELECT id FROM sessions WHERE expires_at < $1 LIMIT $2)
	`
	deleteProcessedOutbox = `
		DELETE FROM outbox_events
		WHERE id IN (
			SELECT id FROM outbox_events
			WHERE processed_at IS NOT NULL AND processed_at < $1
			LIMIT $2
		)
	`
)

// DeleteExpiredOneTimeTokens removes tokens that can no longer be redeemed and
// reports how many rows it deleted.
func (s *Store) DeleteExpiredOneTimeTokens(ctx context.Context, before time.Time) (int64, error) {
	return s.deleteBatched(ctx, deleteExpiredTokens, before)
}

// DeleteExpiredSessions removes sessions whose refresh token has already
// expired. Revoked sessions are kept until then so a sign-out stays visible in
// the account's history.
func (s *Store) DeleteExpiredSessions(ctx context.Context, before time.Time) (int64, error) {
	return s.deleteBatched(ctx, deleteExpiredSessions, before)
}

// DeleteProcessedOutboxEvents removes delivered outbox rows, keeping everything
// that is still pending or waiting on a retry.
func (s *Store) DeleteProcessedOutboxEvents(ctx context.Context, before time.Time) (int64, error) {
	return s.deleteBatched(ctx, deleteProcessedOutbox, before)
}

func (s *Store) deleteBatched(ctx context.Context, statement string, before time.Time) (int64, error) {
	var deleted int64
	for batch := 0; batch < cleanupBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return deleted, err
		}

		tag, err := s.pool.Exec(ctx, statement, before, cleanupBatchSize)
		if err != nil {
			return deleted, err
		}

		affected := tag.RowsAffected()
		deleted += affected
		if affected < cleanupBatchSize {
			break
		}
	}
	return deleted, nil
}
