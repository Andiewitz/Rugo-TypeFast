# Test documentation

How the suites in [`/tests`](../../tests/) are organised, and what each one is for.

## Which suite do I add a test to?

| The change… | Goes in |
| --- | --- |
| Is one function, rule, or component | `tests/unit` or beside the code |
| Crosses HTTP and the database | `tests/int` |
| Is a full user journey through the running app | `tests/e2e` |
| Concerns throughput or memory under load | `tests/k6` |
| Concerns a dependency dying | `tests/failure` |
| Concerns who can reach what | `tests/security` |

Go unit tests sit next to the package they test (`*_test.go`), not in `/tests`.

## Commands

```bash
npm test              # Go unit tests and course tests
npm run test:ci       # Go unit tests with the race detector
npm run test:int:user # HTTP and database tests
npm run test:lesson   # course rules only
```

Integration tests need PostgreSQL: `npm run db:up`. They create, migrate, and empty their own `_test` database, so no migration step is needed.

## Conventions

- Name Go tests `*_test.go`; use table tests for validation and error cases.
- Name end-to-end files `<flow>.spec.ts` and load scripts `<scenario>.k6.js`.
- Every fixed bug gets a test that fails before the fix.
- Tests must pass alone and in any order.
- Use synthetic data and dedicated accounts; never point a suite at production.
- Keep slow, external, and failure-injection suites out of the default run.

## Per-system pages

- [User service](user-service.md) — layers, commands, covered flows
- [Observability](observability.md) — live stack checks

## Adding a suite

1. Pick the folder matching the question the suite answers.
2. Add the runner and config at the nearest module.
3. Add a root script so one command runs it.
4. Update this page and the suite's `README.md`.
