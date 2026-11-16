# Systems

One page per system: what it does, where it lives, and what happens when it fails.

| System | Location | Runtime | Page |
| --- | --- | --- | --- |
| Course app | `client/app` | Browser, React 19 + Vite | [`client-app.md`](client-app.md) |
| Website | `client/web` | Static, Astro | [`../../client/web/README.md`](../../client/web/README.md) |
| User service | `services/user` | Go 1.27.4, PostgreSQL 18 | [`user-service.md`](user-service.md) |
| Typing course rules | `client/app/src` | Runs in the browser | [`learning-path.md`](learning-path.md) |
| Observability | `services/observability` | Docker Compose | [`observability.md`](observability.md) |

Health checks for the user service: `GET /healthz` and `GET /readyz` on port `3001`.

## Who owns dependencies

- Client workspaces declare their own in `package.json`; npm hoists to the root `node_modules` and records versions in `package-lock.json`.
- The Go service declares its own in `services/user/go.mod`; the test module in `tests/go.mod`.
- `go.work` ties the Go modules together for local work.
- Container images build from `services/user/Dockerfile`.

## Adding a system

1. Create the directory with its own manifest and lockfile.
2. Node workspace: add it to `workspaces` in the root `package.json`.
3. Go module: add it to `go.work` and record the module path.
4. Declare dependencies there, never at the repository root.
5. Write a page here covering runtime, ownership, health checks, and failure behavior.
6. Add tests in the matching `/tests` folder and document the commands.
