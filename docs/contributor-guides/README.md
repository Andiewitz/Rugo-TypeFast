# Contributor guide

Everything you need to run the project, change it, and get the change merged.

## 1. Install the tools

| Tool | Version | Why |
| --- | --- | --- |
| Node.js | 22.23.4 or newer | The two client workspaces |
| npm | 11 or newer | Workspace installs |
| Go | 1.27.4 | The user service and its tests |
| Docker with Compose | current | PostgreSQL and Mailpit |

## 2. Set up once

```bash
npm install
cp .env.example .env
set -a; . ./.env; set +a
```

Node dependencies install once from the repository root; each workspace declares its own. Go dependencies live in `services/user/go.mod` and `tests/go.mod`.

The API never reads `.env` itself, so export the values into your shell before a local Go run.

## 3. Run what you are changing

```bash
npm run db:up      # PostgreSQL and Mailpit
npm run migrate:up # apply database migrations
npm run dev:user   # API on :3001
npm run dev:app    # course app on :5173
npm run dev:web    # marketing site on :4321
```

Health checks: http://localhost:3001/healthz and http://localhost:3001/readyz. Email lands in Mailpit at http://localhost:8025.

`docker compose up --build` runs the full container stack instead.

## 4. Where the code lives

| Path | Contents |
| --- | --- |
| `client/app` | React course app (`@kit/app`) |
| `client/web` | Astro site (`@kit/web`) |
| `services/user` | Go service: accounts, sessions, roles, audit, email |
| `tests` | Course, integration, load, failure, and security tests |
| `docs` | Everything else you are reading |

## 5. Check your work

CI runs these on every pull request, so run them first:

```bash
npm run typecheck
npm run lint
npm run test
npm run test:ci             # adds the race detector
npm run db:up && npm run test:int:user
```

The pipeline, the required checks, releases, and the deploy process are in [CI/CD](ci-cd.md). Day-to-day Go detail — migrations, key generation, admin commands — is in [Go services](go-services.md).

## 6. Merge it

1. Branches off `main`, one concern per commit.
2. Use the prefixes: `Add`, `Fix`, `Refactor`, `Update`, `Document`.
3. Open a pull request into `main` and wait for the five checks.
4. Update the matching page under `docs/` in the same change when behavior moves.

Never commit secrets, `.env`, `.secrets/`, or production data.

## Where to write things down

| You have… | Put it in |
| --- | --- |
| A bug you can reproduce | `docs/issues` |
| A design for work that is not started | `docs/plans` |
| An incident that reached users | `docs/post-mortems` |
| A change to how a system works | `docs/systems` |
| A change to how tests are written | `docs/tests-docs` |

Every folder has a `_template.md`. Copy it, keep the headings, fill them in.

## Versioning

The version lives in the root `package.json` and follows `x.y.z`:

- `x` — a large or breaking update
- `y` — a new feature
- `z` — a bug fix

The app shows it under **Settings → Build Version**. Bump it in the same commit as the change it describes:

```bash
npm version minor --no-git-tag-version
```

Workspace packages like `@kit/app` keep their own private versions; they are not the product version.
