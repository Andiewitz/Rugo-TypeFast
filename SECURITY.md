# Security policy

## Report a problem privately

Use GitHub's private reporting: **Security** tab → **Report a vulnerability**.

Do not open a public issue for a security problem, and do not describe it in a pull request. Private reporting keeps the details hidden until there is a fix.

## What is in scope

| Area | Examples |
| --- | --- |
| User service | Sign-in bypass, session or token forgery, authorization gaps, account takeover, email link abuse |
| Client app | Script injection, unsafe storage of tokens, exposure of one user's data to another |
| Pipeline and deploy | Secret exposure, image or workflow misconfiguration, privilege escalation |
| Dependencies | A vulnerable package that is actually reachable from this code |

## What is not a finding

These are known, deliberate choices. Reporting them wastes your time:

- `/metrics` has no authentication. It listens on `127.0.0.1:9091` by default, never on the public API port, and is meant for a private network.
- Access tokens stay valid until they expire. Sign-out revokes only the session; suspension and password reset take effect through the revocation snapshot, which other replicas pick up within `REVOCATION_REFRESH_INTERVAL`.
- A token minted in the same second as a ban is accepted. Issued-at times have one-second resolution, and the alternative is locking out someone who just signed in.
- Rate limits are per process and reset on restart.
- Local development defaults are intentionally weak: `MAILER=log`, `SMTP_TLS=none`, and an ephemeral JWT key.
- default PostgreSQL and Mailpit credentials in `compose.yaml` are for local use only.

## Response

This is a one-person project, so there is no guaranteed response time. Expect an acknowledgement when the report is read, a decision on severity, and a fix in `main` before any public write-up. Please give reasonable time for a fix before disclosing.

## Supported versions

`main` only. There are no release branches, and no version older than the current one is patched.

## Safe testing

Test against your own local instance. Do not run denial-of-service tests, automated scanners with aggressive settings, or anything against a deployment you do not own. Never access data that is not yours; if you can read another account's data, stop and report it.

## Fixing it yourself

Security fixes are welcome as pull requests — after the report, not instead of it. See [`docs/contributor-guides/README.md`](docs/contributor-guides/README.md) for the workflow.
