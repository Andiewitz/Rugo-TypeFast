# Rugo

Rugo teaches typing: 13 chapters, 65 levels, ten short rounds per level, with speed and accuracy tracked as you go.

This repository holds everything needed to run it: the course app, a small marketing site, the Go service that owns accounts and sessions, and the tooling that tests and ships all of it.

## The parts

| Folder | What it is | Language |
| --- | --- | --- |
| `client/app` | The course itself | React 19, Vite, TypeScript |
| `client/web` | Public website | Astro |
| `services/user` | Accounts, login, sessions, roles, email | Go |
| `tests` | Course tests, integration tests, load tests | Node, Go, k6 |
| `docs` | How everything works | Markdown |

## Run it locally

You need Node.js 22.23.4 or newer, Go 1.27.4, and Docker.

```bash
npm install
npm run db:up      # starts PostgreSQL and Mailpit in Docker
npm run migrate:up # creates the database tables
npm run dev:user   # the API, on port 3001
npm run dev:app    # the course app, on port 5173
```

Open http://localhost:5173 and start a lesson.

| Service | URL | What it is |
| --- | --- | --- |
| App | http://localhost:5173 | The typing course |
| API | http://localhost:3001 | Accounts, sessions, roles |
| Mailpit | http://localhost:8025 | Catches outgoing email |
| Metrics | http://localhost:9091/metrics | Prometheus counters and timings |

`docker compose up --build` runs the whole stack instead, including migrations.

The API does not read `.env` on its own. Copy `.env.example` to `.env`, then load it before starting:

```bash
cp .env.example .env
set -a; . ./.env; set +a
```

## Test it

```bash
npm test              # Go unit tests and course tests
npm run test:ci       # Go unit tests with the race detector
npm run test:int:user # full HTTP and database tests (needs npm run db:up)
```

Integration tests build their own throwaway database ending in `_test` and empty it before each run, so they never touch your development data.

## Names in this repository

You will see four names that all mean this project. This is what each one is:

- **Rugo** — the product, and what users see.
- **`Rugo-TypeFast`** — the repository name on GitHub.
- **`type-fast`** — internal infrastructure label: the Compose project, the Docker image, and the npm package.
- **`kit`** — older internal prefix, still on the database name, the refresh cookie, the JWT issuer, and the Prometheus namespace (`kit_user_*`).

Renaming the last two would touch running systems — database, cookies, token checks, dashboards — so they stay until there is a reason to migrate deliberately.

## Documentation

- [`docs/README.md`](docs/README.md) — pick a task, get the right page
- [`docs/systems/user-service.md`](docs/systems/user-service.md) — how accounts and sessions work
- [`docs/contributor-guides/ci-cd.md`](docs/contributor-guides/ci-cd.md) — tests, releases, and deploys
- [`AGENTS.md`](AGENTS.md) — house rules for changes
