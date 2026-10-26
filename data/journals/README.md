# Evidence-backed journal discovery registry

Discovery targeting is separate from current metric qualification. Importing these targets never formally includes a new paper or borrows an old journal title's JIF for a renamed title.

- `historical-seed.json` preserves the original 208-row seed unchanged: 199 previously prepared targets plus 9 lower-threshold historical entries. It is **not** the complete core universe.
- `registry.json` adds **362 discovery-only evidence groups** compiled from every actual curated baseline journal membership with recorded checksum-valid issuer ISSNs: **7,376 BioAI / 316 LLM / 63 World Model memberships**, with no unexplained misses. There are 570 versioned registry rows, not 570 distinct journals.
- Exact preexisting issuer names or explicitly verified seed aliases restore **32 additional topic routes** (10 LLM / 22 World Model) where those historical records omit ISSNs. Only existing identifiers are reused; metric facts and years are not copied. Provenance is recorded in `resolvedTopicRouteRecords`; this does not change paper metadata.
- Medical Image Analysis is included using its recorded ISSNs and historical JIF 14, with **metric year 0/unknown retained**. Knowledge-Based Systems and Expert Systems with Applications include their recorded nested issuer identity evidence. None is presented as freshly qualified.
- Discovery-only entries have `qualificationStatus=historical-curated-discovery`, `evidence.discoveryEnabled=true`, exact original `discoveryTopics`, and `metricVerified=false`; current/new-paper qualification remains pending. Original numeric facts, source cautions, correction precedence, source record IDs, source shard hashes, and canonical metadata hashes remain attached.
- `coverage.json` lists explicit missing-identity and conference/series exclusions. PMLR conference records are not JIF-journal targets; the actual journal *Proceedings of the IEEE* is not excluded merely because its name contains “Proceedings”. No fuzzy title matching or invented ISSNs are used. Record-local missing ISSNs remain reported even when a separate, already evidenced registry identity provides the discovery route.

Reproduce or verify offline from the committed baseline and preserved seed:

```sh
python3 tools/journalregistry/compile.py
python3 tools/journalregistry/compile.py --check
```

`tools/importbaseline -mode import` verifies registry/coverage checksums and registers each name/version before paper import. Same-version replays do not overwrite previous evidence. Runtime core queries route valid ISSNs by `discoveryTopics`; unknown metrics do not disable explicitly evidenced historical discovery. Expanded feeds remain unrestricted by the incomplete known core identities. Missing recorded ISSNs are not invented; some such names may already be covered independently by the preserved seed.
