# Contributor Guides

## Prerequisites

- Node.js 22.23.4 or newer
- npm 11 or newer
- Go 1.27.4
- Docker with Compose

## Setup

```bash
npm install
cp .env.example .env
set -a
. ./.env
set +a
```

All Node dependencies are installed once from the repository root. Each workspace declares its own dependencies. Go dependencies live in `services/user/go.mod` and `tests/go.mod`.

## Repository layout

- `client/app` - React application built with Vite and TypeScript (`@kit/app`)
- `client/web` - Astro website (`@kit/web`)
- `services/user` - Go identity and account service
- `tests` - unit, integration, end-to-end, load, failure, and security tests
- `docs` - issues, plans, system notes, post-mortems, and test documentation

## Development commands

```bash
npm run db:up          # PostgreSQL and Mailpit
npm run migrate:up     # apply migrations
npm run dev:app        # React app
npm run dev:web        # Astro site
npm run dev:user       # Go user service on :3001
```

The service health endpoints are `http://localhost:3001/healthz` and `http://localhost:3001/readyz`. Mailpit is available at `http://localhost:8025`.

Run the complete container stack with `docker compose up --build`.

See [Go services](go-services.md) for toolchain, migrations, key generation, administration, and testing details.

## Before opening a pull request

CI runs these checks on every pull request, so run them locally first:

```bash
npm run typecheck
npm run lint
npm run build
npm run test:user
```

The pipeline, required checks, releases, and the single-VM deploy are described in [CI/CD](ci-cd.md).

Integration tests require PostgreSQL:

```bash
npm run test:int:user
```

Never commit secrets, credentials, `.env`, `.secrets/`, or production data.

## Versioning

The product version is the `version` field in the root `package.json` and follows semantic versioning:

- `x` (major) - a large or breaking update
- `y` (minor) - a new feature
- `z` (patch) - a bug fix or small correction

The app injects that value at build time and shows it under **Settings → Build Version**, so a running build always says which release it came from. Bump the version in the same commit as the change it describes:

```bash
npm version minor --no-git-tag-version
```

Workspace packages such as `@kit/app` keep their own `private` versions; they are not the product version.

## Where to document work

- Bug or regression: `docs/issues`
- Upcoming design or implementation work: `docs/plans`
- Incident follow-up: `docs/post-mortems`
- Architecture or ownership changes: `docs/systems`
- Test strategy or harness changes: `docs/tests-docs`
