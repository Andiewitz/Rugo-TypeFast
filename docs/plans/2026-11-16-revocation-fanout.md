# Plan: share the ban list across servers

- Status: approved — build before running a second API instance
- Owner: engineering
- Created: 2026-11-16
- Target: first multi-instance deployment

## Problem

Each API instance keeps its own list of banned accounts in memory and reloads it from PostgreSQL every `REVOCATION_REFRESH_INTERVAL` (5s).

With one server that is perfect. With two, the wrong one keeps letting a banned user in for up to five seconds, and every instance polls the database forever just to hear about changes.

## Decision

PostgreSQL stays the source of truth. Redis carries news of changes. The in-memory list stays the thing checked on every request.

Nothing in the request path talks to Redis, so Redis being down can slow propagation but can never block a sign-in or let a banned user through.

## How it works

```
ban request
   │
   ▼
PostgreSQL transaction          status = suspended
   │                            sessions revoked
   │                            revoked_before = now()
   │                            audit event
   ├── commit
   │
   ▼
PUBLISH revocations <event>     Redis, fire and forget
   │
   ├──▶ instance A: update memory
   ├──▶ instance B: update memory
   └──▶ instance C: update memory

safety net: every instance also reloads the full list from PostgreSQL
            on startup, on reconnect, and on a slow timer
```

### Rules

1. **Commit before publishing.** Publishing inside the transaction would announce bans that then roll back.
2. **A failed publish does not fail the ban.** The database is already correct. Log it and let the timer catch up.
3. **The event carries state, not instructions.** Send the account ID, status, and marker timestamp. Each instance applies it; it never asks Redis what the list is.
4. **Reload, never guess.** On connect, on reconnect, and on a decode error, read the full list from PostgreSQL.
5. **Keep the timer.** It is the proof that nothing was missed. It can slow down (5s → 60s) but it must not disappear.
6. **Keep the rule that makes this safe:** a ban is monotonic. `revoked_before` only moves forward, so duplicate, late, or reordered events all converge on the same answer.

### What stays out of scope

- **Rate limits.** They stay per process. Sharing them is a separate change with a separate cost.
- **Session storage.** Refresh tokens stay in PostgreSQL. Redis is for the ban list only.
- **Redis Cluster.** One instance with persistence is enough until there is a measured reason.

## When to build it

| Signal | Action |
| --- | --- |
| One API instance | Nothing. Current design is correct. |
| Two or more instances | Build this plan. |
| Ban propagation measured slower than ~1s matters | Build this plan sooner. |
| Multi-region writes | Revisit the transport; see below. |

## Failure behavior

| Failure | Result |
| --- | --- |
| Redis down | Bans still commit; propagation falls back to the timer |
| Instance restarts | Loads the full list from PostgreSQL before serving |
| Event lost | Timer corrects it within one interval |
| Event delivered twice | Harmless; the state is identical |
| Events arrive out of order | The marker timestamp decides; older loses |
| PostgreSQL down | Existing snapshot keeps serving; nothing new is learned |

## Steelman: gossip instead of a broker

The alternative is to give every node a copy of the list and let nodes talk to each other: no central broker, no extra hop, no single point of failure, and it tolerates a network split because each node keeps working with what it knows.

That is a genuinely better shape when:

- writes can originate in more than one region,
- a broker is a latency problem (cross-region round trip on every ban),
- or partition tolerance is a hard requirement, not a preference.

The cost is that you now own conflict resolution. For a ban list the merge rule is easy — highest timestamp wins, because bans only move forward — but you also need node membership, anti-entropy (nodes comparing notes to find missed events), tombstone cleanup, and an answer to "why did node 3 think this account was active?".

For one identity service writing bans in one place, gossip is a distributed-systems project to avoid a five-second delay. The design above gets the same delay down to milliseconds with one extra service and no new protocol.

If two regions ever write bans, take the gossip idea seriously: a CRDT keyed by account ID with a last-writer-wins timestamp is then the simpler system, not the more complex one.

## Implementation

Four commits, each shippable on its own. Nothing changes for a single instance until the last one.

### Step 1 — The registry learns to publish and to merge

Files: `internal/revocation/registry.go`, `registry_test.go`.

```go
// Event is what travels over the bus. It carries state, never instructions.
type Event struct {
	UserID    uuid.UUID `json:"user_id"`
	Before    time.Time `json:"before"`
	Suspended bool      `json:"suspended"`
}

type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

func (r *Registry) SetPublisher(p Publisher)          // nil means local only
func (r *Registry) Apply(event Event)                 // merge a remote event
```

`Revoke`, `Restore`, and `MarkIssuedBefore` publish after updating memory, with a 250 ms timeout, and log a warning on failure. They never return an error to the caller: the database already holds the truth.

`Apply` merges:

- newer `Before` replaces older,
- equal `Before` and `Suspended` true wins (fail closed),
- older events are ignored,
- an event for an unknown account is added, not rejected.

After this step the behavior is identical; the hook is simply unused.

### Step 2 — A Redis bus package

Files: `internal/revocation/bus/redis.go`, `bus/redis_test.go`.

```go
type Bus struct{ /* client, channel */ }

func New(url, channel string) (*Bus, error)
func (b *Bus) Publish(ctx context.Context, event revocation.Event) error
func (b *Bus) Listen(ctx context.Context, onEvent func(revocation.Event), onGap func())
```

- `Publish` serialises the event and publishes to the channel. Errors are returned, never fatal.
- `Listen` subscribes, decodes each message, and calls `onEvent`.
- On any subscribe or receive error, `Listen` calls `onGap` — which reloads the full list from PostgreSQL — then resubscribes with backoff. A dropped connection therefore costs one reload, not a permanent blind spot.
- A message that will not decode is logged and skipped; it must not kill the subscription.

New dependencies: `github.com/redis/go-redis/v9` at runtime, `github.com/alicebob/miniredis/v2` in tests only.

### Step 3 — Configuration and wiring

Files: `internal/config/config.go` (+ test), `cmd/api/main.go`, `.env.example`, `compose.yaml` (Redis service, `--profile` off by default), `compose.prod.yaml`.

| Variable | Default | Meaning |
| --- | --- | --- |
| `REVOCATION_BUS` | `none` | `none` keeps today's polling only; `redis` turns the bus on |
| `REDIS_URL` | empty | Required when the bus is `redis` |
| `REVOCATION_CHANNEL` | `kit:revocations` | Channel name, so two environments can share a Redis |

Wiring in `main.go`, after the initial `Refresh`:

1. `none`: exactly what runs today.
2. `redis`: build the bus, `SetPublisher`, `go bus.Listen(ctx, registry.Apply, refresh)`.

The ticker stays in both cases.

Failure to reach Redis at startup is logged and the service still starts. Redis is on the path to *speed*, never to correctness.

### Step 4 — Write it down

Update `docs/systems/user-service.md`: the ban section, the configuration table, and the failure table gain the bus. Set this plan to `approved` when work starts.

## Tests

| Level | Test | Asserts |
| --- | --- | --- |
| Unit | `Revoke` publishes `Suspended: true` | Local update and event agree |
| Unit | `MarkIssuedBefore` publishes without suspension | Password reset advertises the cut-off, not a ban |
| Unit | `Restore` publishes the lift | Reactivation reaches other instances |
| Unit | Publisher returns an error | State still updated, no panic, warning logged |
| Unit | No publisher set | Behaves exactly as today |
| Unit | `Apply` older then newer | Newest `Before` wins |
| Unit | `Apply` equal `Before`, one suspended | Suspended wins |
| Unit | `Apply` duplicate event | Idempotent |
| Unit | `TokenRevoked` during `Apply`, run with `-race` | No data race, no torn read |
| Unit | Bus round trip on miniredis | Published event arrives decoded |
| Unit | Bus `Listen` after a server restart | `onGap` fires, subscription resumes |
| Unit | Bus receives malformed JSON | Logged and skipped, listening continues |
| Unit | Bus `Publish` with the server down | Returns an error, does not block the caller |
| Unit | Config: `REVOCATION_BUS=redis` without `REDIS_URL` | Startup fails with a clear message |
| Unit | Config: defaults | `none`, so nothing changes for existing deployments |
| Integration | Two registries, one bus, ban on A | B refuses the token in under one second |
| Integration | Ban with Redis unreachable | Ban commits, timer applies it, no request fails |
| Integration | Restart a registry | Full reload from PostgreSQL before serving |
| Manual | Two API processes, real Redis, admin ban | Second process returns `401 token_revoked` |

Sleep-free where possible: use a poll-until-true helper with a deadline instead of `time.Sleep` in the propagation tests.

## Acceptance

- With `REVOCATION_BUS=none`, every existing test passes untouched.
- With `redis`, a ban enforces everywhere in under one second on a local network.
- With Redis stopped, no request fails and no ban is lost.
- Nothing in the request path calls Redis, proven by a test that removes the bus and still serves.

## Risks

| Risk | Handling |
| --- | --- |
| Redis becomes a hard dependency by accident | Wire it lazily; log and continue on startup failure |
| A missed message hides a ban | Timer stays, reconnect triggers a full reload |
| Out-of-order delivery | Monotonic merge rule; suspended wins a tie |
| Two environments sharing one Redis | Configurable channel |
| Event flood | Bans are rare; no batching needed |
| Flaky time-based tests | Deadline polling, never fixed sleeps |
