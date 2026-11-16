# CI/CD

GitHub Actions runs the checks on every pull request and, once a host exists, publishes release images and deploys them to one VM.

## Pipeline at a glance

| Workflow | Trigger | Does |
| --- | --- | --- |
| `ci.yml` | Pull request to `main`, manual | Typecheck, lint, lesson tests, Go tests with `-race`, integration tests against PostgreSQL, dependency and secret scans, Docker image build |
| `release.yml` | Tag `v*.*.*` | Checks the tag against `package.json`, publishes `ghcr.io/andiewitz/type-fast-user` tagged `v1.2.3` and `sha-<commit>` |
| `deploy.yml` | Manual, or called by `release.yml` | SSH to the host and runs `scripts/deploy.sh` for one image tag |

`deploy.yml` refuses to run until the repository variable `DEPLOY_ENABLED` is `true`, so the pipeline is complete before any machine exists.

## Pull request checks

| Job | Command it runs | Catches |
| --- | --- | --- |
| Client | `npm run typecheck`, `lint`, `test:lesson`, `build` | Type errors, lint, course-rule regressions, broken builds |
| Go | `gofmt`, `go vet`, `npm run test:ci`, `go build` | Formatting drift, vet findings, data races, compile breaks |
| Integration | `go -C tests test ./int/user/... -count=1` | HTTP and database behavior against real PostgreSQL 18 |
| Security | `npm audit --audit-level=high`, `govulncheck@v1.9.0`, `gitleaks` | Known vulnerable dependencies and committed secrets |
| Image | `docker build` with no push | A Dockerfile that no longer builds |

Run the same checks locally before pushing:

```bash
npm run typecheck
npm run lint
npm run test
npm run test:ci             # Go tests with the race detector
npm run db:up && npm run test:int:user
npm audit --audit-level=high
go -C services/user run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

The integration job starts `postgres:18-alpine` as a service and points `TEST_DATABASE_URL` at it. The harness creates and migrates `kit_test` itself.

### Branch protection

CI cannot protect a branch by itself. In **Settings → Branches**, add a rule for `main` with:

- Require a pull request before merging.
- Require status checks: `Client`, `Go`, `Integration`, `Security`, `Image`.
- Require branches to be up to date before merging.

## Releasing

The tag and the Build Version shown in Settings must match, so the release workflow rejects a mismatched tag.

```bash
npm version minor --no-git-tag-version
git commit -am "Update version to 1.1.0"
git tag v1.1.0
git push origin main v1.1.0
```

The release workflow publishes the image. It does not touch a server by itself; the deploy job it calls is still gated by `DEPLOY_ENABLED` and by the `production` environment's reviewers.

## Deploying to the VM

One host runs `db`, `migrate`, and `user` from [`compose.prod.yaml`](../../compose.prod.yaml). Images come from GHCR, so the host never builds anything.

### One-time host setup

1. Create a Linux VM with Docker Engine and the Compose plugin, for example a 2 vCPU / 4 GB instance.
2. Create a deploy user and a directory:

```bash
sudo adduser --disabled-password --gecos "" deploy
sudo usermod -aG docker deploy
sudo mkdir -p /srv/type-fast && sudo chown deploy:deploy /srv/type-fast
```

3. Put three files in `/srv/type-fast`: `compose.prod.yaml`, `scripts/deploy.sh`, and `.env.production` (never committed).

```bash
cd /srv/type-fast
# copy compose.prod.yaml and scripts/deploy.sh from the repository
cp /path/to/.env.production .
```

4. `.env.production` holds the deployment values:

```bash
POSTGRES_USER=kit
POSTGRES_PASSWORD=<generated>
POSTGRES_DB=kit
DATABASE_URL=postgres://kit:<generated>@db:5432/kit?sslmode=disable
JWT_PRIVATE_KEY=<output of cmd/keygen>
JWT_KEY_ID=prod-1
JWT_ISSUER=kit-user
JWT_AUDIENCE=kit-api
ACCESS_TOKEN_TTL=15m
REFRESH_TOKEN_TTL=720h
COOKIE_SECURE=true
COOKIE_DOMAIN=
APP_ORIGIN=https://app.example.com
PUBLIC_APP_URL=https://app.example.com
MAILER=smtp
SMTP_ADDR=smtp.example.com:587
MAIL_FROM=no-reply@example.com
SMTP_TLS=starttls
INTERNAL_SERVICE_TOKEN=<generated>
RATE_LIMIT_ENABLED=true
LOG_LEVEL=info
RETENTION_INTERVAL=1h
RETENTION_OUTBOX_WINDOW=168h
```

5. Give the host read access to the image. Either make the GHCR package public, or log in once with a token that has `read:packages`:

```bash
echo "<PAT>" | docker login ghcr.io -u <github-user> --password-stdin
```

6. Put TLS in front. The API listens on `127.0.0.1:3001` only, so point a reverse proxy at it — Caddy needs two lines:

```
app.example.com {
    reverse_proxy 127.0.0.1:3001
}
```

### GitHub configuration

No SSH key exists yet. Generate a dedicated one on your machine and keep the private half in GitHub:

```bash
ssh-keygen -t ed25519 -C "type-fast-deploy" -f ~/.ssh/type-fast-deploy -N ""
ssh-copy-id -i ~/.ssh/type-fast-deploy.pub deploy@<host>
```

Then in **Settings → Secrets and variables → Actions**:

| Kind | Name | Value |
| --- | --- | --- |
| Secret | `SSH_KEY` | Contents of `~/.ssh/type-fast-deploy` (the private key) |
| Secret | `SSH_HOST` | Hostname or IP of the VM |
| Secret | `SSH_USER` | `deploy` |
| Variable | `DEPLOY_PATH` | `/srv/type-fast` |
| Variable | `DEPLOY_ENABLED` | `true` once the host is ready |

Create an environment named `production` under **Settings → Environments** and add yourself as a required reviewer. That is what turns a tag push into an approved deploy instead of an automatic one.

### First deploy

Run **Deploy** from the Actions tab with `environment: production` and `image_tag: v1.0.0`, or push a tag and approve the waiting job.

The script does the work in this order:

1. `pg_dump` into `backups/` (skip with `SKIP_BACKUP=1`).
2. `docker compose pull` the image for `user` and `migrate`.
3. `migrate up` as a one-shot container, so a failed migration stops before the API is replaced.
4. `docker compose up -d --no-deps user`.
5. Poll `http://127.0.0.1:3001/readyz` for 60 seconds.
6. Record the tag in `.last-good-tag`, or roll back to the previous tag and exit non-zero.

### Rollback

Dispatch **Deploy** again with the tag you want to return to. The image still exists in GHCR, and the script rewrites `.last-good-tag` after a healthy start. Migrations are forward-only: rolling the API back does not undo a schema change, so restore the matching `backups/*.sql` if a migration is the problem.

### Backups

`scripts/deploy.sh` leaves `backups/kit-<timestamp>.sql` on the host before each deploy. Copy them off the machine on a schedule — a backup on the same disk does not survive the disk:

```bash
# from your machine
rsync -av deploy@<host>:/srv/type-fast/backups/ ./type-fast-backups/
```

## What is not automated

- End-to-end browser tests: no Playwright runner is configured.
- Load tests: `tests/k6` is opt-in and needs a running API.
- Alerting: Prometheus and Grafana run in the Compose observability profile, but the production host does not ship them yet.
- Secret scanning of anything pushed outside a pull request.

## Costs

GitHub Actions is free for public repositories and included in the free minutes for private ones; the five pull request jobs finish in a few minutes each. GHCR storage for one image per release is negligible. The VM is the only recurring cost.
