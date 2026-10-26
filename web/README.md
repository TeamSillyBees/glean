# Glean Web

Chinese-first research catalog and reading workspace. Built with **React 19 + TypeScript + Vite**, **Tailwind CSS v4**, owned **shadcn/ui-style components over real Radix primitives**, Lucide and TanStack Query.

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
- There is **no silent demo fallback**. Backend errors have explicit retry states. Deterministic, clearly marked mock fixtures exist only in tests; backend mock runs and mock papers remain visibly labeled.
- `src/lib/filters.ts` maps the original imported topic-specific dimension names to query parameters. Values and classifications come from backend facets, not frontend heuristics. World-model `display_*` values are imported from the original HTML taxonomy by the backend importer.
- Ordered historical filters: paper type → direction → venue scope → task → model role → year → review status → venue. **Scientific review evidence** is separate from the user's **unread / reading / read** state.
- Preferences load before the first paper query. Save is explicit. Personal hidden papers are excluded by default, with a separate recoverable hidden view. Neither action modifies original metadata.
- Formal online and issue dates are displayed separately. Missing dates remain pending; acceptance/submission/index dates never substitute for publication dates.
- Evidence URLs accept only HTTP(S), opened with `noopener noreferrer`; raw evidence is rendered as escaped text.
- Session epochs prevent late refresh, logout, stale 401 and delayed JSON body races from crossing account boundaries.

## Interface

Three-topic navigation; literal search and keyboard focus shortcut (`⌘K` / `Ctrl+K`); backend counts; active filter chips; pagination; personal reading/hidden states; evidence drawer; source health; scan history; query templates; versioned venue registry. Narrow screens use Radix dialogs for navigation and filtering. Dialogs support Escape, focus trapping and focus return. Reduced-motion preferences are honored.

## Component ownership

`src/components/ui/` follows shadcn/ui's open-code component pattern, adapted for this workspace. The components use actual `radix-ui` Dialog, Select, Tabs, Tooltip and Slot primitives, with Tailwind utilities and CVA variants. `components.json` keeps standard shadcn aliases and theme configuration; components remain editable source rather than a proprietary widget abstraction.

Documentation consulted:
- [shadcn/ui Tailwind v4](https://ui.shadcn.com/docs/tailwind-v4)
- [shadcn/ui Vite installation](https://ui.shadcn.com/docs/installation/vite)
- [Radix Dialog accessibility](https://www.radix-ui.com/primitives/docs/components/dialog)
- [Vite getting started](https://vite.dev/guide/)

## Verification scope

Vitest + Testing Library exercise authenticated querying, exact taxonomy transport, saved preference restoration, loading/error/empty transitions, repeated detail dismissal, read/hidden mutations and restoration, read-only monitoring, keyboard search, and session-race isolation. All network data in these tests is explicitly mocked. These are functional DOM tests, not a claim of browser screenshot QA or live backend end-to-end coverage. Real Chromium execution was unavailable in the assigned environment due to the previously reported socket permission denial; it was not retried or bypassed.
