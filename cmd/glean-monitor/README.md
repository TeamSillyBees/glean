# Glean mock monitor runner

This command exercises real monitor orchestration with synthetic source records and deterministic mock review. It never calls source APIs or an LLM, never inserts mock papers into the historical catalog, and never enables a scheduler.

## Offline (default)

```sh
go run ./cmd/glean-monitor --node 11 --scenario incremental
```

`baseline` produces four mock candidate versions; `incremental` produces twelve versions across two runs; `failure` demonstrates a malformed source without advancing its checkpoint. The default is in-memory. `--full` includes the synthetic raw evidence and mock review objects.

## Persist mock runs for the actual monitor API

1. Apply the existing application migrations and local admin seed using the normal project workflow. Use the same PostgreSQL database as the server.
2. Choose a Snowflake node ID distinct from every running application/importer instance. Obtain the ID of an existing enabled operator account for audit.
3. Set `INITRA_MONITOR_DATABASE_URL` privately. Do not commit a DSN or pass it as a command-line argument.
4. Explicitly opt in:

```sh
# INITRA_MONITOR_DATABASE_URL must already be configured privately.
go run ./cmd/glean-monitor \
  --node 11 \
  --persist-mock \
  --operator YOUR_EXISTING_USER_ID \
  --scenario incremental \
  --at 2026-10-26T12:00:00Z
```

Remote databases require `sslmode=verify-full`. Local loopback-only development can explicitly add `--allow-local-insecure` when its DSN uses `sslmode=disable`; this never relaxes remote TLS checks.

The command writes `glean_monitor_state` under the default `glean-monitor-mock-v1` key used by the running API. Refresh the authenticated views:

- `GET /api/v1/monitor/runs`
- `GET /api/v1/monitor/sources`
- `GET /api/v1/monitor/templates`

Repeating the same command and `--at` is idempotent: existing runs/versions are not duplicated. Interrupted runs resume their existing ID, fixed end watermark and saved cursor. To demonstrate a later scan, deliberately choose a later anchor. Existing state is never reset or deleted by this command. A concurrent active worker is rejected by the durable lease; retry after that worker finishes.

A completed mock review means only that the software stage ran. Every mock result remains `isMock=true`, `scientificReviewed=false`, `inclusionStatus=pending`. `publishedThrough` tracks projection into the mock monitor snapshot, not scientific inclusion or journal publication.

## Optional scheduler integration

The application integration is explicitly opt-in through `monitor.scheduler_enabled` (default `false`) and requires the existing task worker and scheduler to be enabled. It registers a one-minute wake-up pulse, **not** a one-minute scan: the database owns the 72-hour UTC anchor, source pagination readiness and backoff. Not-due pulses do not update state. Queue-level automatic retries are disabled; persisted per-source retry budgets apply instead.

The module methods `RegisterTasks(task.Registry)` and `RegisterSchedule(task.Scheduler)` register capabilities without starting threads themselves. The existing application lifecycle owns worker/scheduler startup and shutdown. Automatic real-source harvesting is rejected in this phase even if a service was manually configured for a real adapter.

## Verification

```sh
go test ./internal/modules/monitor ./cmd/glean-monitor -count=1
go test -race ./internal/modules/monitor ./cmd/glean-monitor -count=1
go vet ./internal/modules/monitor ./cmd/glean-monitor
```

With an explicitly configured test PostgreSQL URL, CI also runs:

```sh
go test ./internal/modules/monitor -run TestMonitorPostgresDurableMockRun -count=1 -v
```

`INITRA_TEST_DATABASE_URL` is used only by the integration test. It applies the actual migration history to a temporary transaction-isolated schema, exercises persistent partial run/resume/incremental/replay, reconstructs API services, checks CAS fencing, and rolls everything back. Without that variable the test explicitly skips; this is not evidence of PostgreSQL validation.
