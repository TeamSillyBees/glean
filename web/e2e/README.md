# Real browser acceptance

The `browser` job in `.github/workflows/ci.yml` is an actual Chromium browser test,
not a jsdom or mocked-network test. It:

- Applies committed migrations to a disposable PostgreSQL 16 database and uses Redis 7.
- Creates two ordinary, non-admin test users with a generated ephemeral password.
- Imports all 39,495 real historical memberships through the actual importer;
  quarantine, missing records or unexpected duplicates fail the job.
- Separately checks the exact curated counts (8,556 / 1,204 / 1,210), the
  curated-default UI, all-records scope, and a genuinely empty new-discovery
  queue. Historical unverified records must not be reclassified as new discoveries.
- Builds the frontend, serves its production bundle via Vite preview and proxies
  to the actual Go HTTP server. The separate container job tests the shipped nginx configuration.
- Exercises desktop and Pixel 7 viewports: login, server-side year/search filters,
  details/evidence, read/hidden mutations, persistence after reload/login and
  isolation between users. Every API request reaches Go and PostgreSQL.
- Searches the year picker and the real venue registry, verifies an actual empty
  query, refreshes real source-health data, and checks that disabled scheduling
  plus no completed collection window stays visibly unknown rather than healthy.
  Historical records must not acquire invented source versions or scan history.
- Uploads viewport and full-page screenshots plus a private Actions HTML report.
  No trace, HAR, auth-state file, token, server log or quarantine is uploaded.
  Before upload, exact generated secrets and JWT-shaped tokens are redacted from
  native report text and its embedded ZIP step logs; upload fails closed if the
  report format cannot be sanitized safely.

External live requests are excluded from normal push/PR CI. Only the manually
dispatched `.github/workflows/live-source-acceptance.yml` requests one EuropePMC
record through the real source adapter and persists it through the
actual monitor/catalog pipeline. The resulting committed IDs drive a second
desktop/mobile browser check: real login and search, real detail HTTP, immutable
source evidence, the official record link, and an explicitly separate mock LLM
review. No paper title or source response is fabricated for this phase. Upstream
unavailability fails this live check honestly; there is no fixture fallback.
The manual job creates isolated accounts without importing the historical catalog.
The historical and live reports/screenshots use separate directories and artifacts.

`npm run test:e2e:list` and `npx tsc -p tsconfig.e2e.json` validate discovery/types
without launching a browser. Actual browser execution is performed on the
sanctioned GitHub Actions runner, not by retrying a denied local browser path.

Passing both phases verifies the full historical catalog and one actual live
EuropePMC response through PostgreSQL and the authenticated UI. It does **not**
prove continuous provider availability, complete live discovery, all other source
pipelines, or genuine LLM analysis. Six-source canaries remain separate and require the `live` Go build tag plus an
explicit environment opt-in; default `go test ./...` excludes them.
Screenshots are review evidence, not an automatic claim of visual acceptance.
