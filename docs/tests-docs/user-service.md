# User service tests

What the suites cover for the Go service at `services/user`.

## Layers

| Layer | Where | Answers |
| --- | --- | --- |
| Unit | `services/user/internal/**/*_test.go` | Does one rule hold? |
| Integration | `tests/int/user` | Does the real HTTP stack plus PostgreSQL behave? |
| Load | `tests/k6/user-session.k6.js` | Does it hold up under traffic? |
| Failure and security | `tests/failure`, `tests/security` | What breaks, and who can get in? |

## Run them

```bash
npm run test:user     # unit
npm run test:ci       # unit with -race
npm run test:int:user # integration (needs npm run db:up)
```

Direct commands:

```bash
go -C services/user test ./...
go -C tests test ./int/user/... -count=1
```

## How integration tests stay safe

- They build the real router, real services, and a real store against PostgreSQL. The database is not mocked.
- They create the database, apply migrations, and truncate `users`, `sessions`, `one_time_tokens`, `audit_events`, and `outbox_events` before each run. Roles and permissions seeded by migration survive.
- `DATABASE_URL` is ignored on purpose, so a development database cannot be truncated by accident. Set `TEST_DATABASE_URL` to choose another scratch database; its name must end in `_test`.
- Email is never sent. The test reads the outbox payload directly to get verification and reset tokens.

## Unit coverage

- **Input**: email, password, display name, and status validation.
- **Secrets**: Argon2id hashing, random tokens, Ed25519 signing and parsing (tampering, wrong audience, wrong key).
- **Rules**: registration, recovery, login outcomes, authorization, account updates.
- **Delivery**: outbox send, retry, and backoff.
- **HTTP**: JSON decoding, CORS, security headers, request IDs, origin checks, rate limits, problem responses.
- **Runtime behavior**: the revocation snapshot and its event merge rules, the Redis bus (round trip, reconnect gap, malformed payload), the retention sweep, email link shape, the metrics listeners, and configuration defaults.

## Integration coverage

- Registration, duplicate-email masking, verification, login, profile read and update.
- Session listing, current-session marker, logout, refresh.
- Refresh rotation, replayed token, session-family revocation.
- Password reset, old password refused, sessions revoked, reset token refused on reuse.
- Admin listing, suspend and activate, role grant and revoke, permission enforcement.
- Internal permission checks, and rejection of a wrong internal token.
- Audit events visible to the account owner.
- CSRF origin rejection, content-type enforcement, weak passwords, invalid tokens.
- Health and readiness, and the absence of a metrics route on the API port.
- Bans: a suspended account is refused with `token_revoked` everywhere, other accounts keep working, reactivation leaves old tokens dead, another replica picks the ban up.
- Cognito identity storage: creation from a subject, duplicate rejection, lookup, linking, re-point conflicts, soft-deleted accounts.
- Retention against real rows: expired tokens and sessions deleted; live and revoked-but-unexpired sessions kept; delivered outbox rows past the window deleted; a backlog larger than one batch fully drained.

## Load

`tests/k6/user-session.k6.js` runs login, account read, refresh, and logout against a dedicated environment with a pre-verified account.

```bash
USER_EMAIL=load@example.com \
USER_PASSWORD='correct horse battery staple' \
BASE_URL=http://localhost:3001 \
APP_ORIGIN=http://localhost:5173 \
k6 run tests/k6/user-session.k6.js
```

One virtual user every seven seconds stays under the rate limits. Raise it only with rate limiting disabled.

## Known gaps

- Browser end-to-end journeys are not automated; no runner is configured.
- Database outage, migration failure, SMTP timeout, graceful shutdown, and overload are described in [`tests/failure`](../../tests/failure/) but not all scripted.
- Secrets scanning runs in CI; dependency scans run locally and in CI.

## Before merging backend work

```bash
go -C services/user vet ./...
go -C services/user test -race ./...
npm run test:int:user
```
