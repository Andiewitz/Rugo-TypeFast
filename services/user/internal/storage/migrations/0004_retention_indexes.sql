-- +goose Up
-- Retention deletes rows by expiry, and every one of these tables gains a row
-- on ordinary traffic: a one-time token per registration or reset, a session per
-- sign-in, an outbox row per email. Without an index the sweep scans the whole
-- table on every tick.
--
-- The outbox index is partial because only delivered rows are ever swept.
CREATE INDEX one_time_tokens_expires_idx ON one_time_tokens (expires_at);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);
CREATE INDEX outbox_events_processed_idx ON outbox_events (processed_at) WHERE processed_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS outbox_events_processed_idx;
DROP INDEX IF EXISTS sessions_expires_idx;
DROP INDEX IF EXISTS one_time_tokens_expires_idx;
