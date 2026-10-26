# Glean Web

Chinese-first research catalog and reading workspace. Built with **React 19 + TypeScript + Vite**, **Tailwind CSS v4**, owned **shadcn/ui open-code components + real Radix primitives**, Lucide and TanStack Query.

## Run locally

Requires Node 24 and npm 11 (the committed lockfile pins resolved package versions).

```bash
cd web
npm ci
cp .env.example .env.local
npm run dev
```

The Vite server binds to `127.0.0.1:5173` and proxies `/api` to the Go server at `http://127.0.0.1:8080`. Override `GLEAN_API_PROXY` for a different local port. Production should serve `dist/` and reverse-proxy `/api` on the same origin; a separate `VITE_API_BASE_URL` requires an explicit backend CORS allowlist.

Initialize the local Go backend account through the repository's documented setup/seed workflow, then sign in with that account. No example password is embedded. Access and refresh tokens remain in module memory, never in browser storage. Reloading the page intentionally requires sign-in again.

```bash
npm run typecheck
npm run lint
npm test
npm run build
npm run preview
```

## Architecture and data boundaries

- All application data comes from `/api/v1/catalog/*` or `/api/v1/monitor/*` using the initra envelope and authenticated API. See `../docs/glean/api-contract.md`.
- The frontend is a query interface. It does not harvest sources, launch scans, invoke LLMs, infer scientific classifications or invent live counts.
- There is **no silent demo fallback**. Backend errors have explicit retry states. Deterministic, clearly marked mock fixtures exist only in tests; only model review may be mock, labeled beside that output rather than labeling real source papers as fake.
- `src/lib/filters.ts` maps the original imported topic-specific dimension names to query parameters. Values and classifications come from backend facets, not frontend heuristics. World-model `display_*` values are imported from the original HTML taxonomy by the backend importer.
- Default scope is backend-owned **curated**, with explicit pending-live and all-record scopes; all includes historical background/out-of-scope records. Topic totals remain labeled as total records, not curated counts. Facet loading, retryable errors and genuinely missing labels are distinct.
- Ordered historical filters: paper type → direction → **AI method (biomedical AI only)** → venue scope → task → model role → year → collection status → specific venue. All 9 biomedical-AI and 8 LLM/world-model controls are visible; searchable exact backend labels keep rare values reachable. **Scientific review evidence** is separate from the user's **unread / reading / read** state.
- Preferences load before the first paper query. Save is explicit. Personal hidden papers are excluded by default, with a separate recoverable hidden view. Neither action modifies original metadata.
- Formal online and issue dates are displayed separately. Missing dates remain pending; acceptance/submission/index dates never substitute for publication dates.
- Evidence URLs accept only HTTP(S), opened with `noopener noreferrer`; raw evidence is rendered as escaped text.
- Session epochs prevent late refresh, logout, stale 401 and delayed JSON body races from crossing account boundaries.

## Interface

Three-topic navigation; literal search and keyboard focus shortcut (`⌘K` / `Ctrl+K`); backend counts; active filter chips; pagination; personal reading/hidden states; evidence drawer; source health; scan history; query templates; versioned venue registry. Narrow screens use Radix dialogs for navigation and filtering. Dialogs support Escape, focus trapping and focus return. Reduced-motion preferences are honored.

## Component ownership

See [component provenance and upstream registry sources](COMPONENTS.md), including the retained MIT notice. `src/components/ui/` follows shadcn/ui's open-code component pattern, adapted for this workspace. The components use actual `radix-ui` Dialog, Select, Tabs, Tooltip and Slot primitives, with Tailwind utilities and CVA variants. `components.json` keeps standard shadcn aliases and theme configuration; components remain editable source rather than a proprietary widget abstraction.

Documentation consulted:
- [shadcn/ui Tailwind v4](https://ui.shadcn.com/docs/tailwind-v4)
- [shadcn/ui Vite installation](https://ui.shadcn.com/docs/installation/vite)
- [Radix Dialog accessibility](https://www.radix-ui.com/primitives/docs/components/dialog)
- [Vite getting started](https://vite.dev/guide/)

## Verification scope

Vitest + Testing Library exercise authenticated querying, exact taxonomy transport, saved preference restoration, loading/error/empty transitions, repeated detail dismissal, read/hidden mutations and restoration, read-only monitoring, keyboard search, and session-race isolation. All network data in these tests is explicitly mocked. These are functional DOM tests, not a claim of browser screenshot QA or live backend end-to-end coverage. Real Chromium execution was unavailable in the assigned environment due to the previously reported socket permission denial; it was not retried or bypassed.

## Real API browser acceptance

`npm run test:e2e:list` lists the desktop/mobile acceptance tests without launching a browser. `npm run test:e2e` is intended for the authorized GitHub Actions runner: it uses a production Vite build/preview, the real Go API, PostgreSQL and real historical imports. The harness owns no API fixtures, route responses or token injection. CI provides isolated ephemeral accounts through `GLEAN_E2E_USERNAME`, `GLEAN_E2E_SECOND_USERNAME`, `GLEAN_E2E_PASSWORD`, and the backend proxy through `GLEAN_API_PROXY`. It records screenshots as CI artifacts, not token-bearing traces/HARs. Do not commit `test-results/`, `playwright-report/` or authentication state. Passing test listing is **not** a claim that browser execution or visual QA passed.

## Container deployment

Build from the repository root with **`web/` as the build context**:

```bash
docker build --pull -t glean-web:local ./web
```

The multi-stage image builds with the official `node:24-bookworm-slim` image, then copies only `dist/` into official `nginx:stable-alpine`. The runtime uses the non-root `nginx` user on port **8080**. `RUN nginx -t` validates configuration during the image build. Upstream image tags intentionally receive maintenance updates; production release tooling should additionally pin verified image digests.

The frontend and Go service must share a **user-defined Docker/Compose network**, and the Go service must be reachable on that network as **`backend:8080`**. Docker's embedded DNS (`127.0.0.11`) is resolved at request time and refreshed every 30 seconds; a backend starting later does not prevent the static frontend from starting. The API should stay on that private service network; only publish the frontend's port to the local host. The repository-level Compose configuration is maintained separately.

For an already-created network named `glean` containing the backend service, a restrictive local run is:

```bash
docker run --rm --name glean-web --network glean \
  --read-only --tmpfs /tmp:rw,noexec,nosuid,size=16m,mode=1777 \
  --cap-drop ALL --security-opt no-new-privileges \
  -p 127.0.0.1:8088:8080 glean-web:local
```

- Open `http://127.0.0.1:8088`. `/api/*` is forwarded to `backend:8080` **without stripping `/api`**, preserving JSON request bodies, query strings and the Bearer header. API errors remain API errors, never an HTML fallback.
- SPA deep links fall back to `index.html`; missing hashed assets return 404. HTML is revalidated; hashed `/assets/` files are cacheable. API responses are not cached.
- `/healthz` checks nginx and the built `index.html` file. The Docker health check does **not** claim database/API readiness or a successful login. Validate the backend independently.
- The image consumes no runtime secrets, tokens, LLM keys or environment substitution. `.env*`, local npm configuration and common key formats are excluded from the build context, and Dockerfile uses an explicit source-copy allowlist. The container intentionally uses same-origin `/api`, so `VITE_API_BASE_URL` is not a runtime switch.
- Nginx uses `/tmp` for its PID and temporary files, permitting a read-only root filesystem. Access logs omit query strings and credentials. Static CSP permits same-origin connections only, while external paper links remain ordinary user-initiated navigation.
- This is a private local-network HTTP example, not an external production deployment. Terminate TLS at a separately configured trusted edge before public exposure; adapt trusted-proxy/forwarded-header settings deliberately rather than trusting arbitrary client headers.

### Authentication onboarding and verification

Initialize a real local account with the backend's setup/seed instructions; no fixed production credentials are supplied by the frontend. The browser uses these exact routes:

| Action | Route | JSON / authentication |
| --- | --- | --- |
| Sign in | `POST /api/v1/auth/login` | `{username,password}`; reads `data.user`, `data.accessToken`, `data.refreshToken` |
| Refresh | `POST /api/v1/auth/refresh` | `{refreshToken}`; public endpoint, rotates the refresh token |
| Query | `GET /api/v1/catalog/topics` (and catalog/monitor views) | `Authorization: Bearer …` |
| Sign out | `POST /api/v1/auth/logout` | `{refreshToken}` plus Bearer access token |

`test/e2e/glean_auth_test.go::TestGleanFrontendAuthContract` verifies the real Go HTTP handlers, JWT issuance/rotation, refresh replay rejection, authenticated topic query, logout and subsequent revocation through `httptest`, using a SQL-mocked database. This is stronger than the frontend's mocked transport tests but **is not a live PostgreSQL/browser end-to-end claim**.

`src/test/deployment.test.ts` checks deployment-file invariants only. In the current executor Docker, Podman and nginx binaries are unavailable, so the container image build, `nginx -t` inside that image, and running reverse-proxy behavior have not been executed here. The build-time validation above runs when the image is actually built. No browser screenshot QA is claimed.

Official references: [Node Docker images](https://github.com/nodejs/docker-node), [nginx Docker image and non-root configuration](https://hub.docker.com/_/nginx), [nginx proxy URI behavior](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_pass), [Docker health checks](https://docs.docker.com/reference/dockerfile/#healthcheck).
