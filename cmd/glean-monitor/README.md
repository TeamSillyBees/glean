# Glean real monitor runner

**Normal operation uses real source APIs, PostgreSQL and catalog ingestion. Model processing uses explicit HTTP adapters and waits for configuration; it never silently falls back to mock.** Official sources are Crossref, PubMed, arXiv OAI-PMH, bioRxiv, Europe PMC and medRxiv. Synthetic sources require an explicit `--demo` flag and are not part of server startup.

## Configure once

Apply the normal application migrations and admin seed, then privately configure:

- `INITRA_MONITOR_DATABASE_URL`: the **same database as the server**. Never commit the DSN or pass it on the command line.
- `INITRA_MONITOR_OPERATOR_ID`: an existing enabled user ID, used for audited state and catalog writes.
- `INITRA_IDGEN_NODE`: a Snowflake node distinct from every running server/importer. `--node` can override it for a separate CLI process.

Remote databases require `sslmode=verify-full`. Loopback development can explicitly use `--allow-local-insecure` with `sslmode=disable`; remote plaintext and endpoint/query/environment overrides are rejected.

```sh
# These commands can acquire sources without model keys; model processing waits for configuration.
go run ./cmd/glean-monitor --doctor

go run ./cmd/glean-monitor --node 11 \
  --sources crossref,pubmed,europepmc \
  --since 2026-10-20T00:00:00Z \
  --until 2026-10-26T00:00:00Z \
  --max-pages 6
```

`--doctor` checks database/actor/registry/source configuration without calling any source API. Error diagnostics produce a nonzero exit status. `--contact` is optional explicit source contact information; no account email is inferred.

The real-source CLI is bounded to three minutes and 1–200 upstream request attempts (`--max-pages`, default 12). Valid observations are **written to the real catalog before model processing**. They are visible in the catalog’s pending-review collection (API `collectionScope=pending`) as real papers; the curated historical collection stays separate. Source authenticity, model stage completion and human approval are independent. See [论文处理流程](../../docs/glean/paper-pipeline.md) for the two model configurations and recovery.

## Resume and scheduled scans

Omitting `--since` and `--until` resumes a pending durable run, or starts a due run using the stored 72-hour UTC schedule and source checkpoints. Explicit windows are half-open, at most 90 days, and cannot end in the future. Reusing the same completed explicit window is idempotent while its run receipt is retained; durable catalog observation identities also prevent duplicate papers after monitor-history compaction.

After changing a topic's search routes, normal resume still uses the original frozen plan. To explicitly scan the **same fixed window with the current plan**, use:

```sh
go run ./cmd/glean-monitor --node 11 --new-plan \
  --sources pubmed,europepmc \
  --since 2026-10-20T00:00:00Z --until 2026-10-26T00:00:00Z \
  --max-pages 6
```

`--new-plan` requires both explicit bounds and cannot be combined with `--demo`, `--doctor`, or `--retry-failed`. An active run must finish first. Old completed runs and their frozen plan remain intact; repeating a completed window with the **same current plan version** remains idempotent. If the new run stops pending, resume normally with its original source set/window, without `--new-plan`. This flag does not enable scheduling, reset retries, or claim complete historical coverage. Route `backfillFrom` bounds are honored within the supplied window; a window entirely before that bound is rejected.

A finite invocation may stop **pending** because its page/time budget is exhausted, pagination remains, or upstream retry time has not arrived. This is not a completed harvest. Resume with the same source set/window or let the configured scheduler continue. Fixed end watermarks and cursors are preserved; acquisition watermarks advance only after all source/topic pagination completes, while review/projection watermarks advance independently after those phases complete. A manual window cannot jump an existing acquisition gap.

Ordinary scans use the three versioned topic queries with a seven-day overlap and a thirty-day first window. Every normal journal-source run performs full core-journal feeds **after** ordinary discovery over the same overlap window. Every tenth run widens only that core-journal window to ninety days; ordinary discovery continues independently. Non-journal providers do not duplicate ISSN core scans. Core feeds retain all journal records rather than silently discarding items by keyword; `coreDiscovery=true` means “core journal discovery, topic relevance unreviewed,” never confirmed topic relevance or scientific inclusion. The registry is an incomplete historical seed, not the full journal universe.

The server's `monitor.scheduler_enabled` integration uses the existing task worker/scheduler lifecycle. Its one-minute wake-up pulse checks the durable 72-hour anchor and retry readiness; it is not a one-minute full scan. Configure the audited actor before enabling it. The `GET /api/v1/monitor/sources` diagnostics report missing actors, disabled scheduling and registry limitations. CLI scans do not enable background scheduling. Scheduled task executions and the real-source CLI both have a three-minute total budget; a shorter parent deadline still applies. Each model stage is limited to 120 seconds and starts only when its configured timeout plus three seconds fits the remaining budget; otherwise it is deferred without consuming an attempt.

The request-attempt budget conservatively counts each source step (including a possible local cursor transition); HTTP requests are serialized per provider, with at least three seconds between starts. bioRxiv/medRxiv share their host gate. 429/503 `Retry-After` applies across that provider; 403 without a permitted retry is recorded as blocked rather than repeatedly probed. Redirects and alternate origins are rejected. No automatic source-client retry loops are enabled.

## Query results and retention

Use the regular authenticated catalog UI/API for source evidence, publication events, versions and reading state. Monitor views are:

- `GET /api/v1/monitor/runs`
- `GET /api/v1/monitor/sources`
- `GET /api/v1/monitor/templates`

Query-level evidence records the compiled route expression, fixed time window, source, and version. It is **not a complete per-request HTTP log**: Crossref executes separate bibliographic seed requests whose union is shown as a readable route expression. Full-window harvest sources do not enforce keyword matching upstream; all returned candidates remain subject to downstream relevance review. Finished runs with unavailable planned sources report `completed_with_gaps`, not complete coverage.

Formal dates come from publisher/source publication events, never from acquisition or index timestamps. Machine output remains `scientificReviewed=false`, `inclusionStatus=pending`; it cannot convert discovery into scientific inclusion.

Full observations and raw evidence are durably preserved in the relational catalog. After confirmed catalog archival, bulky pipeline materials/results can leave monitor hot state and be read back from the catalog for inspection or retry. Completed receipts use the existing retention budget (normally 2,000 receipts and 100 completed runs, plus the active run). Unprojected or unfinished processing work retains its queue state; the receipt limit is not a cap on unfinished backlog. A permanent per-record identity conflict or invalid record is quarantined with raw evidence and a safe reason, while other records continue. Quarantined records are never labeled published: `handledThrough` includes isolated records, while `publishedThrough` remains conservative until their resolution. Unchanged quarantines are not retried on every overlap. Catalog uniqueness remains the long-term deduplication authority.

## Explicit offline fixtures

Fixtures exist only for development/testing:

```sh
go run ./cmd/glean-monitor --demo --node 11 --scenario incremental
```

`baseline`, `incremental` and `failure` simulate source transitions without network calls. The old persistent fixture helper is available only with **both** `--demo --persist-mock`; it uses a separate mock state key and never writes fake papers to the real catalog. It is not normal application mode.

## Verification

```sh
go test ./internal/modules/monitor ./cmd/glean-monitor -count=1
go test -race ./internal/modules/monitor ./cmd/glean-monitor -count=1
go vet ./internal/modules/monitor ./cmd/glean-monitor
```

With `INITRA_TEST_DATABASE_URL`, `TestMonitorRealPipelinePostgres` validates real database state, the real catalog publisher, partial-page query visibility, resume and version identity in an isolated rollback schema. Its external source is a test fixture; separate guarded source canaries validate real upstream API responses. A skipped database test is not database validation.

### Bounded real end-to-end canary

This operational check is excluded from normal push/PR CI. The separate `Manual live source acceptance` Actions workflow runs only via explicit `workflow_dispatch`; it creates isolated migrations/test-only accounts, performs one real Europe PMC request, and verifies that observation through the authenticated desktop/mobile browser. It never uses `continue-on-error` or fixture fallback. For a deliberate local check after migrations/seed (operator 7 below must actually exist):

```sh
go run ./cmd/glean-monitor --node 18 --operator 7 --sources europepmc \
  --since 2026-10-23T00:00:00Z --until 2026-10-24T00:00:00Z \
  --max-pages 1 --page-size 1 --require-observation --allow-local-insecure > live-monitor.json
```

The JSON report includes `status`, actual committed `observations` with paper/observation/run IDs, canonical catalog `title`, `observationTitle`, official source URL, actual `llmReview` runtime mode, and independent `llmIsMock`/`scientificReviewed` flags. Each observation also reports the latest persisted `pipelineStatus` and `pipelineStages` (name, status, attempts), not the initial source-metadata snapshot. With models disabled, real acquisition reports `staged_pipeline` / `awaiting_configuration`, never mock success; model and extra material attempts remain zero. `bounded-partial` with a committed observation is success, not a claim of full coverage. `blocked`, `quarantined`, a database error, or no committed observation with `--require-observation` exits nonzero; no fixture fallback is allowed. `--page-size` requests 1–1000 items, but some providers impose fixed page sizes (bio/medRxiv and arXiv OAI). A successful older canary does not establish current upstream availability.

## 显式恢复已结束的暂时失败

普通相同窗口调用仍保持幂等，不自动重新打开 `completed_with_errors`。当已知暂时错误（例如 503）耗尽预算且来源恢复后，可使用输出中的失败运行 ID：

```sh
go run ./cmd/glean-monitor --node 11 --sources crossref,pubmed,europepmc \
  --retry-failed '<failed-run-id>' --max-pages 6 --allow-local-insecure
```

- 必须配置原运行的**完全相同源集**，不能同时指定 `--since`、`--until`、`--doctor` 或 `--demo`。原检索模板版本必须仍受支持。
- 创建关联的恢复运行（`retryOf` / `retriedBy`），保留原运行、失败分类和时间证据；使用原窗口、期刊集、分页游标及成功阶段，不扩大扫描范围。
- 只有持久化标为可重试的失败来源获得一次新预算。永久错误、单条隔离、403 持久阻断及旧版本未分类的失败不据字符串猜测为暂时错误。
- 严格遵守原运行及当前来源的 `Retry-After`/退避，不清除访问阻断。已有其他运行或有效 worker 租约时拒绝抢占；状态写入使用 CAS。
- 重复同一 `--retry-failed` 只恢复同一关联运行。若该恢复运行再次耗尽预算，不会用重复原命令无限重试；需检查原因后明确指定新的失败运行 ID。
- 已移出保留历史的运行无法从此入口恢复；不臆造其游标。来源获取不等于科学审核，恢复不会自动解除隔离或把机器结果改为正式纳入。

恢复运行沿用原周期编号，不额外消耗“每十次扫描”的宽窗口补漏计数。报告以 `selectedRunId` 指向本次恢复目标，即使之后已有新运行也不会借用其成功状态或观察记录。`retryCandidateKeys` 固定记录恢复起点的候选证据键；正常投影后原文仍由目录观察记录持久保存，监控的工作缓存/`candidateKeys` 仍按既有策略压缩，并非永远保存全部内存副本。运行历史仍受配置的保留数量限制。
