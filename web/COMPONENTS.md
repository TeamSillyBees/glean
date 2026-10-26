# UI component provenance

Glean uses shadcn/ui's **open-code** model: component source is maintained in this repository, not a fictitious `shadcn-ui` runtime dependency. `components.json` configures the `new-york` style, TypeScript, Tailwind v4 CSS tokens, aliases and Lucide icons.

## Official sources checked 2026-10-26

- [`Button`, official new-york-v4 registry](https://ui.shadcn.com/r/styles/new-york-v4/button.json): `src/components/ui/button.tsx` adapts its CVA variants, typed React props, Radix Slot `asChild`, `data-slot` / `data-variant` / `data-size` composition. Glean changes spacing, color tokens and supported sizes, while preserving focus and disabled semantics.
- [`Popover`, official new-york-v4 registry](https://ui.shadcn.com/r/styles/new-york-v4/popover.json): `src/components/ui/popover.tsx` adapts the Root / Trigger / Portal / Content wrappers. Glean removes animation dependencies and unused header helpers and uses a larger rounded surface.
- [`Command documentation`](https://ui.shadcn.com/docs/components/radix/command): searchable facet selectors compose the owned Popover source with real `cmdk` keyboard/list primitives. `filter-combobox.tsx` adds exact backend facet labels/counts, literal string search, bounded option rendering and a clear selection. It does not classify papers or synthesize missing taxonomy.
- [`Vite installation`](https://ui.shadcn.com/docs/installation/vite) and [`Tailwind v4`](https://ui.shadcn.com/docs/tailwind-v4): path aliases, `cn` (`clsx` + `tailwind-merge`), CSS tokens and component ownership follow these conventions.

Dialog, Select, Tabs and Tooltip are locally tailored wrappers using **actual Radix primitives** from `radix-ui`, not static lookalikes. Their source is owned here, with focus trapping/return, keyboard navigation, portal behavior and accessibility provided by Radix. They were not blindly overwritten by a CLI generation pass. The custom drawer is built from Dialog; the light violet reading palette and Chinese content are application design, not a claim that upstream ships this layout.

Upstream shadcn/ui MIT notice is retained in `LICENSE.shadcn-ui`. Application tests cover exact taxonomy transport, async session isolation and dialog interactions. Real browser acceptance uses the actual Go API in GitHub CI; unit fixture mocks are not production data.
