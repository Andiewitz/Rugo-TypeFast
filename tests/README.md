# Tests

Every test lives here, split by how much of the system it touches.

| Folder | Answers the question |
| --- | --- |
| [`unit/`](unit/) | Does one function or component do what it claims? |
| [`int/`](int/) | Do the HTTP layer and the database work together? |
| [`e2e/`](e2e/) | Does a real user journey work end to end? |
| [`k6/`](k6/) | How does it behave under load? |
| [`failure/`](failure/) | What happens when a dependency dies? |
| [`security/`](security/) | Can someone get in who should not? |

## Run them

```bash
npm test              # Go unit tests and course tests
npm run test:ci       # Go unit tests with the race detector
npm run test:int:user # HTTP and database tests against a scratch database
```

Integration tests need PostgreSQL running:

```bash
npm run db:up
```

The harness creates its own database, applies migrations, and empties it before each run. It refuses a database whose name does not end in `_test`, so your development data is safe.

## Rules

- A test must pass alone and in any order.
- Never commit credentials, tokens, or production fixtures.
- Load and failure tests are opt-in; they never run in the default suite.
- Every fixed bug gets a test that fails without the fix.

How the suites are organised and what they cover is in [`/docs/tests-docs`](../docs/tests-docs/).
