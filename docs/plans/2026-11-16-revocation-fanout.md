# Plan: share the ban list across servers

- Status: draft — build this when there is more than one API instance
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

## Validation when implemented

- Ban on instance A, assert instance B rejects within one second.
- Kill Redis, ban, assert the timer still applies it.
- Restart an instance, assert it loads the list before serving.
- Publish a duplicate and an out-of-order event, assert the final state is correct.
- Load test: assert no request path reaches Redis.
