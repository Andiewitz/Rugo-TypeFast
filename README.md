# Type Fast

A typing course with a React app, an Astro site, and a Go identity service backed by PostgreSQL.

## What is here

| Path | What it is |
| --- | --- |
| `client/app` | React 19 + Vite typing course (`@kit/app`) |
| `client/web` | Astro marketing site (`@kit/web`) |
| `services/user` | Go identity service: accounts, sessions, roles, audit, email |
| `tests` | Lesson, integration, load, failure, and security suites |
| `docs` | Issues, plans, system notes, post-mortems, test documentation |

## Quick start

Requires Node.js 22.23.4+, Go 1.27.4, and Docker with Compose.

```bash
npm install
npm run db:up        # PostgreSQL and Mailpit
npm run migrate:up   # apply database migrations
npm run dev:user     # API on :3001
npm run dev:app      # app on :5173
```

Mailpit catches outgoing email at http://localhost:8025. `docker compose up --build` runs the whole stack instead.

The API does not read `.env` itself: copy `.env.example`, then use `set -a; . ./.env; set +a` before a local Go run.

## Tests

```bash
npm test              # Go unit tests and lesson tests
npm run test:int:user # PostgreSQL-backed integration suite (needs npm run db:up)
```

## Documentation

- [`AGENTS.md`](AGENTS.md) - workflow rules for changes in this repository
- [`docs/README.md`](docs/README.md) - map of all documentation
- [`docs/systems/user-service.md`](docs/systems/user-service.md) - identity service architecture, configuration, and failure behavior
- [`docs/contributor-guides/README.md`](docs/contributor-guides/README.md) - setup and contribution workflow
