# Real browser acceptance

The `browser` job in `.github/workflows/ci.yml` is an actual Chromium browser test,
not a jsdom or mocked-network test. It:

- Applies committed migrations to a disposable PostgreSQL 16 database and uses Redis 7.
- Creates two ordinary, non-admin test users with a generated ephemeral password.
- Imports all 39,495 real historical memberships through the actual importer;
  quarantine, missing records or unexpected duplicates fail the job.
- Builds the frontend, serves its production bundle via Vite preview and proxies
  to the actual Go HTTP server. The separate container job tests the shipped nginx configuration.
- Exercises desktop and Pixel 7 viewports: login, server-side year/search filters,
  details/evidence, read/hidden mutations, persistence after reload/login and
  isolation between users. Every API request reaches Go and PostgreSQL.
- Uploads viewport and full-page screenshots plus a private Actions HTML report.
  No trace, HAR, auth-state file, token, server log or quarantine is uploaded.

`npm run test:e2e:list` and `npx tsc -p tsconfig.e2e.json` validate discovery/types
without launching a browser. Actual browser execution is performed on the
sanctioned GitHub Actions runner, not by retrying a denied local browser path.

Passing this suite verifies the historical catalog and authenticated UI against a real database.
It does **not** prove live-provider availability, complete live discovery or genuine
LLM analysis; source canaries and review-pipeline checks are separate. Screenshots
are review evidence, not an automatic claim of visual acceptance.
