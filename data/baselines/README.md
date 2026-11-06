# Historical baseline snapshots

These are the complete three historical catalogs, not generated demo fixtures: **39,495 topic memberships** (31,399 biomedical AI; 4,059 LLM/Agent; 4,037 biomedical world models).

- `manifest.json`: provenance, source/count/ID hashes, classification distributions, privacy redaction counts, and gzip shard checksums.
- `*.ndjson.gz`: all normalized import records, retaining topic IDs and scientific source evidence.
- `*.display-taxonomy.json.gz`: the delivered reader's display-only grouping table, not new scientific judgments.
- `testdata/`: small real-record regression fixtures; not a substitute for the complete snapshots.

Read [the baseline guide](../../docs/guides/baseline.md) for exact preservation boundaries and explicit, idempotent database import. Full unredacted source JSON is in a separate ignored private archive, not Git. Run `go run ./tools/importbaseline -mode verify` from the repository root to validate the complete baseline without touching a database.
