# User service

The Go service behind accounts. It stores who a user is, proves who they are when they sign in, keeps their sessions, controls what they may do, and sends the emails that make all of that possible.

- Location: `services/user`
- Language: Go 1.27.4
- Database: PostgreSQL 18
- Port: `3001`
- Health: `GET /healthz` · readiness: `GET /readyz`

## What it owns

Accounts, passwords, sessions, roles and permissions, account audit history, verification and reset emails, and the trusted permission check other services call.

The client app holds no identity of its own. It sends a bearer token and the API decides.

## Run it

```bash
npm run db:up        # PostgreSQL and Mailpit
npm run migrate:up   # apply migrations
npm run dev:user     # start the API
```

| Command | Does |
| --- | --- |
| `go run ./cmd/api` | HTTP API, email worker, cleanup sweeper |
| `go run ./cmd/migrate up` | Apply migrations |
| `go run ./cmd/migrate down` | Roll back one migration |
| `go run ./cmd/migrate status` | Show where the database is |
| `go run ./cmd/admin …` | List users, change roles and status |
| `go run ./cmd/keygen` | Write an Ed25519 signing key to `.secrets/` |

All four binaries ship in one image; the default entrypoint is `/app/api`.

## How the pieces fit

```
client app or site
   │  HTTPS, JSON, bearer token, refresh cookie
   ▼
cmd/api
   ├── internal/httpapi        routes, middleware, handlers
   ├── internal/register       sign-up and email verification
   ├── internal/login          password check and session start
   ├── internal/session        tokens, rotation, revocation
   ├── internal/recovery       password reset
   ├── internal/account        profile, sessions, audit history
   ├── internal/authorization  admin users and roles, permission checks
   ├── internal/outbox         sends queued email
   ├── internal/retention      deletes rows nobody needs
   └── internal/storage        PostgreSQL and migrations
        │
        ├─► PostgreSQL
        └─► SMTP
```

It is one deployable unit with clean internal seams: feature packages depend on small interfaces, and `cmd/api/main.go` wires them together.

## How signing in works

Two tokens do the work.

- **Access token** — a 15-minute Ed25519-signed pass. Any replica can check it without asking the database.
- **Refresh token** — a 30-day key in an HttpOnly cookie. It gets you a new access token.

### Signing up

1. Email is normalised and the password checked against the rules.
2. The password is hashed with Argon2id; the user is stored as `pending`.
3. A verification token is hashed and stored, and the email is queued in the same transaction.
4. The email worker sends the link.
5. Following the link consumes the token, activates the account, and records an audit event.

Registering an address that already exists returns the same `202` as a new one. Otherwise the endpoint would tell strangers which addresses have accounts.

### Signing in

1. Email and password are verified. An unknown address still runs a dummy hash, so both failures take equally long.
2. Accounts that are not `active` are refused.
3. Roles and permissions are loaded and copied into the token.
4. A session row is created holding a SHA-256 hash of the refresh token.
5. `kit_refresh` is set for `/v1/auth`; the access token and user are returned in JSON.

### Refreshing

The cookie looks like `<session-id>.<random-secret>`. Every refresh replaces the stored hash and issues a new access token, so a stolen cookie is only useful until the real client refreshes.

Presenting a refresh token that was already used means one of the two copies is an attacker: the entire session family is revoked, and both sides must sign in again.

### Signing out, bans, and why tokens die early

An access token cannot be recalled once signed; it is checked offline. So the service keeps a **revocation snapshot**: a small in-memory list of banned accounts and a timestamp per account.

A request fails with `401 token_revoked` when the account is suspended, or when the token was issued at or before the account's `revoked_before` marker.

Suspending an account does three things in one transaction: sets `status = 'suspended'`, revokes every session, and stamps `revoked_before = now()`.

- Suspension is absolute. Even a token minted after the ban is refused, which is what stops a refresh from restoring access.
- Reactivating lifts the suspension but keeps the marker, so new tokens work and pre-ban tokens stay dead.
- The replica handling the ban applies it immediately; others reload every `REVOCATION_REFRESH_INTERVAL` (default `5s`).
- With `REVOCATION_BUS=redis`, replicas also broadcast each change on a Redis channel, so the rest apply it in milliseconds instead of waiting for the next reload. Redis only carries news: if it is down, bans still commit and the timer closes the gap, and no request ever waits on it.
- A ban written by the `admin` CLI bypasses the bus, because the CLI is a separate process. Other replicas learn about it on their next refresh.
- The check is a map lookup, so it adds no database load and grows with banned accounts, not users.
- Issued-at times have one-second resolution, so a token minted in the same second as a ban is allowed. The window is under a second, and the trade is deliberate: never lock out someone who just signed in.
- A failed refresh keeps the previous snapshot, so a database hiccup cannot quietly un-ban anyone. A failed first load stops startup.
- Bans apply to this API only. Cognito's `AdminUserGlobalSignOut` and `AdminDisableUser` are not called yet; doing so would also kill Cognito refresh tokens and add an AWS SDK dependency.

Users can also revoke one session from `/v1/account/sessions`, and signing out revokes the current one.

### Resetting a password

1. A reset request is accepted for unknown addresses too, so the endpoint reveals nothing.
2. The token is hashed, single-use, and expires in 30 minutes.
3. Following the link updates the password, revokes every session, and stamps `revoked_before` in one statement.
4. Access tokens issued before the reset stop working at once on the replica that handled it, and within `REVOCATION_REFRESH_INTERVAL` on the rest.

## API reference

Rate limits are token buckets per client address: the refill rate and the burst.

| Method | Path | Access | Limit |
| --- | --- | --- | --- |
| `GET` | `/healthz` | none | none |
| `GET` | `/readyz` | none | none |
| `POST` | `/v1/auth/register` | none | 1 per 12s, burst 5 |
| `POST` | `/v1/auth/email/verify` | none | 1 per 3s, burst 10 |
| `POST` | `/v1/auth/email/resend` | none | 1 per 12s, burst 5 |
| `POST` | `/v1/auth/login` | none | 1 per 6s, burst 10, plus a per-account throttle |
| `POST` | `/v1/auth/refresh` | refresh cookie | 1 per 2s, burst 30 |
| `POST` | `/v1/auth/logout` | bearer | none |
| `POST` | `/v1/auth/password/forgot` | none | 1 per 12s, burst 5 |
| `POST` | `/v1/auth/password/reset` | none | 1 per 6s, burst 5 |
| `GET` | `/v1/account` | `account.read` | none |
| `PATCH` | `/v1/account` | `account.update` | none |
| `GET` | `/v1/account/sessions` | `sessions.read` | none |
| `DELETE` | `/v1/account/sessions/{id}` | `sessions.revoke` | none |
| `GET` | `/v1/account/events` | `account.read` | none |
| `GET` | `/v1/admin/users` | `users.read` | none |
| `PATCH` | `/v1/admin/users/{id}/status` | `users.manage` | none |
| `POST` | `/v1/admin/users/{id}/roles` | `roles.manage` | none |
| `DELETE` | `/v1/admin/users/{id}/roles/{role}` | `roles.manage` | none |
| `GET` | `/v1/admin/roles` | `roles.manage` | none |
| `POST` | `/internal/v1/authz/check` | `X-Internal-Token` | 1 per 50ms, burst 20 |

Errors use `application/problem+json` with `type`, `title`, `status`, `code`, `detail`, and `request_id`.

## Rules the API enforces

- Only `APP_ORIGIN` may call with credentials, and state-changing requests carrying an `Origin` header must match it.
- JSON is capped at 1 MiB and rejects unknown fields.
- `X-Request-ID` is accepted or generated, and echoed on every response.
- Security headers: `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, a restrictive permissions policy, and `Strict-Transport-Security` whenever `COOKIE_SECURE` says this is an HTTPS deployment.
- Login is limited per address and per account, so one host cannot spray a single address.
- Argon2 parameters read from a stored hash are bounded; a tampered or corrupt hash cannot force unbounded memory and CPU.
- Passwords are 12 to 128 characters, hashed with Argon2id at 64 MiB, 3 iterations, parallelism 2.

## Roles and permissions

| Role | Can |
| --- | --- |
| `user` | `account.read`, `account.update`, `sessions.read`, `sessions.revoke` |
| `admin` | everything above, plus `users.read`, `users.manage`, `roles.manage` |

Permissions are baked into the access token, so a role change lands on the next sign-in or refresh. Middleware checks permissions on every route.

## The internal permission check

Other trusted services ask:

```http
POST /internal/v1/authz/check
X-Internal-Token: <shared secret>
Content-Type: application/json

{"user_id":"<uuid>","permission":"users.read"}
```

It reads the account's current status and roles, so it is fresher than a client's token. The endpoint is disabled when `INTERNAL_SERVICE_TOKEN` is empty, returns `401` for a wrong token, is rate limited per address, and production refuses to start if the token still holds the value published in `.env.example`.

## Data

Migrations live in `internal/storage/migrations`, are embedded in the binary, and run through goose. Add the next number; never edit an applied migration.

| Table | Holds |
| --- | --- |
| `users` | email, password hash, status, display name, timestamps, `cognito_sub`, `revoked_before` |
| `roles`, `permissions`, `role_permissions`, `user_roles` | who may do what |
| `sessions` | refresh-token hash, family, client metadata, expiry, revocation |
| `one_time_tokens` | hashed verification and reset tokens |
| `audit_events` | actor, subject, action, IP, metadata |
| `outbox_events` | queued email with retry state |

Indexes cover user status, active sessions, token lookup, audit history, pending outbox work, token and session expiry, and delivered outbox rows — the last three exist for the cleanup sweeper.

### Cognito identity

Cognito proves who someone is; this service still owns the account. The canonical record is always a `users` row.

- `users.cognito_sub` holds the subject. A partial unique index allows at most one live account per subject and leaves local accounts unlinked.
- Accounts created through Cognito store a placeholder in `password_hash`, so password login always fails for them.
- `postgres.Store` exposes `CreateUserFromCognito`, `GetUserByCognitoSub`, and `LinkCognitoSub`. Errors distinguish `cognito_sub_taken`, `cognito_sub_already_linked`, `email_exists`, and `cognito_identity_not_found`.
- Linking the same subject twice is idempotent; re-pointing an account at a different subject is refused.
- Soft-deleted accounts keep their `cognito_sub`. Signing in again returns `cognito_sub_taken` until an operator restores the account or clears the link, so a deleted identity cannot quietly become a second account.

## Email

Handlers write the message into `outbox_events` in the same transaction as the state change, so a failed email never rolls back a registration and a rolled-back registration never sends mail.

The worker claims pending rows with `FOR UPDATE SKIP LOCKED`, sends through the configured mailer, then marks success or schedules a retry with backoff. Delivery is **at least once**: a crash between sending and updating can duplicate a message.

| Setting | Behaviour |
| --- | --- |
| `MAILER=log` | Records metadata only, never the body |
| `MAILER=smtp` | Sends through `SMTP_ADDR` as `MAIL_FROM`; Mailpit catches it locally |

Links put the token in the URL fragment (`/verify-email#token=…`), so it never appears in a request line, access log, or `Referer` header. The client reads `location.hash` and posts the token.

Bodies contain those tokens, so the hop matters: `SMTP_TLS=starttls` refuses a relay that cannot upgrade, `opportunistic` upgrades when offered, `none` sends in clear text. Development defaults to `none` because local Mailpit has no certificate; a clear-text relay must never carry these links.

## Housekeeping

Expired tokens and sessions are dead the moment they expire, but nothing else deletes them. The sweeper runs inside the API process every `RETENTION_INTERVAL` and removes:

- expired one-time tokens,
- expired sessions,
- outbox rows delivered more than `RETENTION_OUTBOX_WINDOW` ago.

Each table is swept in batches of 1000 with a bounded number of batches per tick, so a large backlog drains over several ticks instead of holding a lock.

Two rows are kept on purpose: a consumed but unexpired token (so replaying it still says "already used") and a revoked but unexpired session (so the sign-out stays visible in account history). Pending outbox rows are never deleted.

`RETENTION_INTERVAL=0` turns the sweep off and production warns at startup, because then nothing removes these rows.

## Metrics

`cmd/api` keeps a Prometheus registry under the `kit_user` namespace and serves it on its own listener, `METRICS_ADDR` (default `127.0.0.1:9091`, `off` disables it). The API port answers `404` for `/metrics`, so a scrape never touches the surface users reach.

| Metric | Type | Labels |
| --- | --- | --- |
| `kit_user_http_requests_total` | counter | `method`, `path`, `status` |
| `kit_user_http_request_duration_seconds` | histogram | `method`, `path` |
| `kit_user_panics_total` | counter | none |

Latency buckets run from 5 ms to 10 s. The `path` label is folded into `health`, `auth_user`, `auth_session`, `account`, `admin`, `internal`, `metrics`, or `other`, so series count stays fixed as traffic and identifiers grow.

A panicking request increments `kit_user_panics_total` but not the request counter, because the recoverer sits outside the logging middleware. Alert on panics directly instead of inferring them from status codes.

No tracing exporter is configured yet.

## Configuration

| Variable | Default | Notes |
| --- | --- | --- |
| `APP_ENV` | `development` | `development`, `test`, or `production`; anything else stops startup |
| `PORT` | `3001` | API port |
| `METRICS_ADDR` | `127.0.0.1:9091` | Prometheus listener, separate from `PORT`; `off` disables it |
| `DATABASE_URL` | local development URL | Required in production |
| `APP_ORIGIN` | `http://localhost:5173` | CORS and origin checks |
| `PUBLIC_APP_URL` | `http://localhost:5173` | Links in verification and reset email |
| `JWT_PRIVATE_KEY` | random in development | Base64 Ed25519 seed or private key; required in production |
| `JWT_KEY_ID` | `dev-1` | Key ID in the token header |
| `JWT_ISSUER` | `kit-user` | Token issuer |
| `JWT_AUDIENCE` | `kit-api` | Token audience |
| `ACCESS_TOKEN_TTL` | `15m` | Must be shorter than the refresh TTL |
| `REFRESH_TOKEN_TTL` | `720h` | Session lifetime |
| `COOKIE_SECURE` | true in production | Also the signal that this deployment is HTTPS |
| `COOKIE_DOMAIN` | empty | Optional cookie domain |
| `MAILER` | `log` | `log` or `smtp` |
| `SMTP_ADDR` | `localhost:1025` | SMTP server |
| `MAIL_FROM` | `no-reply@kit.local` | Envelope sender |
| `SMTP_TLS` | `none` in development, `starttls` in production | `starttls`, `opportunistic`, or `none` |
| `INTERNAL_SERVICE_TOKEN` | empty | Enables the internal permission check |
| `RATE_LIMIT_ENABLED` | `true` | In-memory limits |
| `REVOCATION_REFRESH_INTERVAL` | `5s` | How often replicas reload the ban list |
| `REVOCATION_BUS` | `none` | `none` keeps bans local; `redis` shares them between instances |
| `REDIS_URL` | empty | Required when the bus is `redis` |
| `REVOCATION_CHANNEL` | `kit:revocations` | Channel name, so two environments can share one Redis |
| `RETENTION_INTERVAL` | `1h` | Cleanup cadence; `0` disables it |
| `RETENTION_OUTBOX_WINDOW` | `168h` | How long delivered email rows are kept |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `AUTO_MIGRATE` | `false` | Apply migrations at API startup |

Generate a signing key from the repository root with `go run ./services/user/cmd/keygen` and keep `.secrets/` out of git. Compose reads optional values from the repository-root `.env`.

## When something goes wrong

| Situation | What happens |
| --- | --- |
| Database unreachable at startup | The API refuses to start |
| Database goes away later | `/readyz` returns `503`; the process stays up |
| JWT key invalid | The API refuses to start |
| Email delivery fails | The outbox row stays pending with backoff; the registration or reset still stands |
| Two refreshes race | The session family is revoked instead of issuing overlapping tokens |
| Process restarts | Rate limits reset; nothing else is lost |
| Ban written elsewhere | Visible within `REVOCATION_REFRESH_INTERVAL`, or at once with the bus |
| Redis unreachable (bus on) | The ban commits and applies locally; other replicas catch up on the next reload |

## Before you deploy

- Bans and forced sign-outs land on the next request, and on other replicas within `REVOCATION_REFRESH_INTERVAL`.
- Permission claims inside a client's token can be up to 15 minutes stale. Use the internal check when freshness matters.
- The refresh cookie is hardcoded to `SameSite=Lax`, which works when app and API share a site. On different registrable domains, add a configurable `SameSite=None; Secure` first.
- Rate limits and audit entries use the TCP peer address, and proxy headers are not trusted. Terminate traffic directly or add trusted-proxy handling before putting a load balancer in front.
- Prometheus must scrape `METRICS_ADDR`, not `PORT`, over a private network.
- Token rotation uses a single Ed25519 key. To rotate, deploy a new key and let old access tokens expire.

## Local operations

```bash
npm run db:up
npm run migrate:up
npm run dev:user
```

Or the full container stack:

```bash
docker compose up --build
```

Admin commands:

```bash
go -C services/user run ./cmd/admin list-users
go -C services/user run ./cmd/admin grant-role <user-id> admin
go -C services/user run ./cmd/admin set-status <user-id> suspended
```

## Related

- [Design plan](../plans/2026-10-27-user-service.md)
- [Contributor guide](../contributor-guides/go-services.md)
- [Test documentation](../tests-docs/user-service.md)
