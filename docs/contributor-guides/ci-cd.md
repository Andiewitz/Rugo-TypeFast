# CI/CD

How code gets checked, released, and deployed.

Three workflows do all of it: `ci.yml` for pull requests, `release.yml` for tags, `deploy.yml` for a server.

## On every pull request

Five jobs run in parallel. Roughly four minutes cold, under two with warm caches.

| Job | Runs | Catches |
| --- | --- | --- |
| Client | typecheck, lint, course tests, build | Type errors, lint, course-rule regressions, broken builds |
| Go | gofmt, vet, tests with `-race`, build | Formatting drift, vet findings, data races, compile breaks |
| Integration | HTTP and database tests against PostgreSQL 18 | Behavior that only appears with a real database |
| Security | `npm audit`, `govulncheck`, secret scan | Known vulnerable dependencies, committed secrets |
| Image | Docker build, no push | A Dockerfile that no longer builds |

Run the same things locally before pushing:

```bash
npm run typecheck
npm run lint
npm run test
npm run test:ci
npm run db:up && npm run test:int:user
```

### Make the checks mandatory

GitHub does not force them by default. In **Settings → Branches → Add rule** for `main`:

1. Require a pull request before merging.
2. Require status checks, and pick `Client`, `Go`, `Integration`, `Security`, `Image`.
3. Require branches to be up to date before merging.
4. Leave "Do not allow bypassing" unchecked on a one-person repository, or you will block yourself.

## Releasing

The version in `package.json` and the tag must match; the workflow refuses otherwise.

```bash
npm version minor --no-git-tag-version
git commit -am "Update version to 1.1.0"
git tag v1.1.0
git push origin main v1.1.0
```

That publishes `ghcr.io/andiewitz/type-fast-user:v1.1.0` and a `sha-<commit>` tag of the same image.

Publishing an image is not deploying it. The deploy job it calls still waits for `DEPLOY_ENABLED`.

## Deploying

One machine runs three containers from [`compose.prod.yaml`](../../compose.prod.yaml): `db`, a one-shot `migrate`, and `user`, plus `redis` alongside the API. Images come from GHCR; the server never builds anything.

### One-time server setup

1. Create a small Linux VM with Docker and the Compose plugin (2 vCPU / 4 GB is plenty).
2. Create a deploy user and directory:

```bash
sudo adduser --disabled-password --gecos "" deploy
sudo usermod -aG docker deploy
sudo mkdir -p /srv/type-fast && sudo chown deploy:deploy /srv/type-fast
```

3. Copy `compose.prod.yaml`, `scripts/deploy.sh`, and your `.env.production` into `/srv/type-fast`.
4. Fill `.env.production` with production values. At minimum:

```bash
POSTGRES_USER=kit
POSTGRES_PASSWORD=<generated>
POSTGRES_DB=kit
DATABASE_URL=postgres://kit:<generated>@db:5432/kit?sslmode=disable
JWT_PRIVATE_KEY=<from cmd/keygen>
COOKIE_SECURE=true
APP_ORIGIN=https://app.example.com
PUBLIC_APP_URL=https://app.example.com
MAILER=smtp
SMTP_ADDR=smtp.example.com:587
SMTP_TLS=starttls
INTERNAL_SERVICE_TOKEN=<generated>
```

5. Let the server pull the image: make the GHCR package public, or run `docker login ghcr.io` once with a token that has `read:packages`.
6. Put TLS in front. The API listens on `127.0.0.1:3001` only, so a reverse proxy terminates HTTPS. With Caddy that is one block:

```
app.example.com {
    reverse_proxy 127.0.0.1:3001
}
```

### Connect GitHub to the server

There is no key yet, so create one:

```bash
ssh-keygen -t ed25519 -C "rugo-deploy" -f ~/.ssh/rugo-deploy -N ""
ssh-copy-id -i ~/.ssh/rugo-deploy.pub deploy@<host>
```

Then in **Settings → Secrets and variables → Actions**:

| Type | Name | Value |
| --- | --- | --- |
| Secret | `SSH_KEY` | the private key file you just made |
| Secret | `SSH_HOST` | server hostname or IP |
| Secret | `SSH_USER` | `deploy` |
| Variable | `DEPLOY_PATH` | `/srv/type-fast` |
| Variable | `DEPLOY_ENABLED` | `true` when the server is ready |

Create an environment named `production` under **Settings → Environments** and add yourself as a required reviewer. That turns a tag into a deploy you approve, not one that happens.

### What a deploy does

Run **Deploy** from the Actions tab with an `image_tag`, or push a tag and approve it. On the server, the script:

1. dumps the database to `backups/` (skip with `SKIP_BACKUP=1`),
2. pulls the image,
3. runs `migrate up` in its own container, so a bad migration stops before the API is replaced,
4. starts the API,
5. polls `http://127.0.0.1:3001/readyz` for 60 seconds,
6. records the tag in `.last-good-tag` — or rolls back to the previous tag and exits non-zero.

### Turning on the revocation bus

One API instance is enough until you run a second one. A single instance polls the database for bans, which costs nothing extra. With two or more, Redis carries each ban to the other instances immediately instead of leaving them to their next poll.

`compose.prod.yaml` already includes the `redis` container: internal only, no published port, persistence off. It carries news, never truth, so losing its contents only means an instance falls back to polling.

To switch the bus on, add these to `.env.production` and deploy again:

```bash
REVOCATION_BUS=redis
REDIS_URL=redis://redis:6379/0
REVOCATION_CHANNEL=kit:revocations
```

`scripts/deploy.sh` starts `redis` with the API, so there is no separate step. If the bus is set to `redis` without `REDIS_URL`, the API refuses to start and says so.

Confirmation is in the logs: startup prints `revocation bus enabled` with the channel, a publish that cannot reach Redis prints `revocation broadcast failed` and still commits the ban, and a dropped subscription prints `revocation bus interrupted`. All three keep polling in place, so a broken bus slows propagation and nothing else.

Bans written with the `admin` CLI on the server do not go through the bus; they reach other instances on the next poll.

### Rolling back

Dispatch **Deploy** with the tag you want. The image is still in GHCR and the script rewrites `.last-good-tag` after a healthy start.

Migrations are forward-only. Rolling the API back does not undo a schema change; if a migration is the cause, restore the matching `backups/*.sql`.

### Backups

`backups/kit-<timestamp>.sql` is written before each deploy. Copy it off the machine on a schedule — a backup on the same disk dies with the disk:

```bash
rsync -av deploy@<host>:/srv/type-fast/backups/ ./type-fast-backups/
```

## Not automated

- Browser end-to-end tests: no runner is configured.
- Load tests: `tests/k6` is opt-in and needs a running API.
- Alerting: Prometheus and Grafana run locally, not on the production server.
- Nothing has been deployed to a real server yet.
