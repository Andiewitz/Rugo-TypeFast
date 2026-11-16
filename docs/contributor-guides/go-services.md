# Go services

Day-to-day guide for `services/user`. The architecture is in [`../systems/user-service.md`](../systems/user-service.md); this page is about doing the work.

## What is where

| Path | Contents |
| --- | --- |
| `services/user/cmd/api` | HTTP server, email worker, cleanup sweeper |
| `services/user/cmd/migrate` | Migration runner |
| `services/user/cmd/admin` | List users, change roles and status |
| `services/user/cmd/keygen` | Ed25519 signing key |
| `services/user/internal` | Feature packages, HTTP layer, storage |
| `tests/int/user` | Black-box HTTP tests |
| `go.work` | Ties the service and test modules together |

The test module is `typefast/services/user/tests` even though it lives at the repository root. That keeps its import path under the service module, so integration tests can import service packages while staying a separate module.

`go.work` is committed on purpose. `go.work.sum` stays local. Do not add machine-specific `replace` directives.

## Set up

```bash
npm install
cp .env.example .env
set -a; . ./.env; set +a
go run ./services/user/cmd/keygen
```

The key lands in `.secrets/jwt_ed25519.key` with `0600` permissions; the command never prints it. Load it when you want tokens to survive a restart:

```bash
export JWT_PRIVATE_KEY="$(cat .secrets/jwt_ed25519.key)"
```

Leave it empty in development and every restart uses a fresh key, which logs everyone out. That is fine locally and never acceptable in production.

The API does not read `.env` itself, so export the values first.

## Run

```bash
npm run db:up
npm run migrate:up
npm run dev:user
```

API on http://localhost:3001, Mailpit on http://localhost:8025. The container stack is `docker compose up --build`, where the `migrate` service must finish before `user` starts.

## Database and migrations

Migrations are SQL files in `services/user/internal/storage/migrations`, embedded in the binary, and run by goose. They look like this:

```sql
-- +goose Up
-- statements

-- +goose Down
-- statements
```

| Rule | Why |
| --- | --- |
| Add the next number (`0005_…`) | Applied migrations are recorded by number |
| Never edit an applied migration | Other databases already ran the old text |
| Always write the `Down` block | Rollback needs it |

```bash
go -C services/user run ./cmd/migrate up
go -C services/user run ./cmd/migrate status
go -C services/user run ./cmd/migrate down   # one migration
```

## Test

```bash
go -C services/user test ./...        # fast unit tests
go -C services/user test -race ./...  # what CI runs
npm run db:up && npm run test:int:user
```

Integration tests create and migrate `kit_test` themselves. They ignore `DATABASE_URL` so your development database cannot be truncated, and they refuse any database whose name does not end in `_test`.

## Add a feature

1. Add or extend a package under `services/user/internal`.
2. Define the interface the consumer needs, in the consumer.
3. Implement persistence in `internal/storage/postgres`.
4. Wire it in `cmd/api/main.go`.
5. Add the route and handler in `internal/httpapi`.
6. Add unit tests beside the package and integration coverage in `tests/int/user` for anything observable.
7. Add a migration if the schema changed.
8. Update [`docs/systems/user-service.md`](../systems/user-service.md) and [`docs/tests-docs/user-service.md`](../tests-docs/user-service.md).

## Error handling

- Return `*apperr.Error` for expected failures, with a stable machine-readable code.
- Use `apperr.Invalid` for validation.
- Never leak internal errors or SQL text to clients; `writeError` maps types to status codes and logs the detail with the request ID.
- Audit security-relevant changes, but do not fail the request when the audit insert fails.

## Security rules

- Never log passwords, raw tokens, authorization headers, or signing keys.
- Hash passwords and one-time tokens; store only hashes.
- Compare shared secrets in constant time.
- Keep state-changing endpoints behind origin checks and authentication.
- Enforce authorization in the service, not just in the UI.
- Rate limit anything that sends email or checks credentials.

## Admin CLI

```bash
go -C services/user run ./cmd/admin list-users
go -C services/user run ./cmd/admin list-roles
go -C services/user run ./cmd/admin grant-role <user-id> admin
go -C services/user run ./cmd/admin revoke-role <user-id> admin
go -C services/user run ./cmd/admin set-status <user-id> suspended
```

It connects with `DATABASE_URL` and writes audit events for its changes.

## When it misbehaves

| Symptom | Cause |
| --- | --- |
| `database system is starting up` | Wait for the Compose health check, then migrate |
| `relation "users" does not exist` | Run `npm run migrate:up` |
| `JWT_PRIVATE_KEY is not set` | Expected in development; set a key for stable sessions |
| Refresh always fails | The client must send `kit_refresh` to `/v1/auth` and use the latest rotated value |
| Mailpit stays empty | Check `MAILER=smtp`, `SMTP_ADDR`, and the outbox worker logs |
| `/metrics` returns 404 | Correct: scrape `METRICS_ADDR` (default `127.0.0.1:9091`) |
