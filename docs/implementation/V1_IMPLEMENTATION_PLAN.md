---
name: Go v1 Implementation Plan
overview: A step-by-step implementation guide for building v1 of the Real-Time Portfolio & Risk Engine in Go with Gin, PostgreSQL, golang-migrate, in-process event processing, OpenAI-backed insights, Zap logging, and Docker—aligned to [docs/design/LLD.md](../design/LLD.md) and [docs/design/HLD.md](../design/HLD.md).
todos:
  - id: add-impl-doc
    content: This file is the canonical implementation plan in-repo (no separate IMPLEMENTATION_PLAN_v1.md)
    status: completed
  - id: scaffold-go
    content: "Phase 1: go mod, cmd/server, Gin health, Zap, layout under internal/"
    status: completed
  - id: db-migrate
    content: "Phase 2-3: docker-compose Postgres + golang-migrate SQL for events/projections/dlq"
    status: completed
  - id: core-pipeline
    content: "Phase 4-8: domain, event store, worker ordering, portfolio+price projections"
    status: in_progress
  - id: api-risk
    content: "Phase 9-11: REST API, risk engine, scenarios"
    status: pending
  - id: snapshots-ai
    content: "Phase 12-15: checkpoints, OpenAI insights, metrics, app Dockerfile"
    status: pending
  - id: tests
    content: "Phase 16: integration + replay equivalence tests"
    status: in_progress
isProject: false
---

# Go v1 implementation plan (followable document)

**Canonical copy:** this file, [docs/implementation/V1_IMPLEMENTATION_PLAN.md](V1_IMPLEMENTATION_PLAN.md), is maintained alongside the code.

**References:** [docs/design/PRD.md](../design/PRD.md), [docs/design/HLD.md](../design/HLD.md), [docs/design/LLD.md](../design/LLD.md).

## Current implementation snapshot (repository state)

| Area | Status |
|------|--------|
| Module / server | `github.com/KevinMReardon/realtime-portfolio-risk`; `go run ./cmd/server` |
| HTTP | `GET /health`; `POST /v1/trades`; `POST /v1/prices` — see LLD §10.1–10.2 for actual JSON |
| Not yet | `GET /v1/portfolios/:id`, risk, scenarios, AI, snapshot-driven recovery |
| DB | `migrations/` through `000003_projection_cursor`; `Makefile` `migrate-up` / `test-integration` |
| Workers | Two pools: trade portfolios + price shards (`cmd/server`); persisted `projection_cursor` (Option B) |
| Projections | `positions_projection`: quantity + `cost_basis` (placeholder `0`); `prices_projection`: symbol/price/`as_of` (Phase 8: price worker + `ApplyPriceBatch` — see Phase 8 tests) |
| Consistency | Separate commits for trade vs price applies; reads must be honest per LLD §14.1 when APIs land |

**Stack (locked for this plan):**

- Go, **Gin** (alternative: Chi—same handlers, different router)
- PostgreSQL, **golang-migrate**
- In-process **goroutine** worker (no Redis/Kafka v1)
- OpenAI API + JSON prompt + response validator
- **Zap** logging; **Prometheus** optional (`/metrics`)
- **Docker** + `docker compose` for Postgres (and optional app)

---

## Phase 0 — Prerequisites and conventions

**Goal:** One machine can run Postgres, run migrations, and `go run` the API.

**Do this:**

1. Install Go (repo currently uses **Go 1.25.x** per `go.mod`), Docker Desktop, `golang-migrate` CLI (Makefile uses CLI).
2. Module path in repo: `github.com/KevinMReardon/realtime-portfolio-risk`.

**Conventions (carry through all code):**

- Timestamps: store **UTC** in DB (`timestamptz`); API ISO-8601 with `Z`.
- Money v1: `NUMERIC(20,8)` or `DECIMAL` in SQL; in Go use `**shopspring/decimal`** or `decimal` via string parsing—avoid `float64` for persisted money.
- IDs: `uuid` for `event_id`, `request_id`; validate UUID format on ingest.
- Config: **environment variables** only for v1 (e.g. `DATABASE_URL`, `OPENAI_API_KEY`, `ORDERING_WATERMARK_MS`) — add Viper later if needed.

**Exit criteria:** `go version`, `docker compose version`, migrate CLI works.

---

## Phase 1 — Repository skeleton

**Goal:** Runnable HTTP server with health check and project layout matching LLD Section 3.

**Layout (as implemented):**

```text
/
  cmd/server/main.go        # DB pool, router, trade + price worker pools, shutdown
  internal/
    config/                 # env: DATABASE_URL, watermark, worker counts, price shards
    domain/                 # envelopes, payloads, validation, Positions (quantities)
    ingestion/              # validate + append
    events/                 # PostgresStore, cursors, trade/price worker pools, apply
    portfolio/              # Aggregate (in-memory apply for projector)
    pricing/, risk/, scenario/, ai/   # stubs for future phases
    api/                    # Gin, trades/prices handlers
    observability/          # Zap
  migrations/000001_*.sql … 000003_projection_cursor.*
  docker-compose.yml
  Dockerfile
  Makefile                  # run, test, migrate-up, test-integration
  go.mod / go.sum
```

**Dependencies (initial):**

- `github.com/gin-gonic/gin`
- `github.com/jackc/pgx/v5` (stdlib-friendly) or `database/sql` + pgx — **recommend pgxpool**
- `go.uber.org/zap`
- `github.com/google/uuid`
- `github.com/shopspring/decimal` (recommended for money)

**Implement:**

- `GET /health` → `200 {"status":"ok"}`.
- Zap production logger in `main`, request ID middleware (generate or propagate `X-Request-ID`).

**Exit criteria:** `go run ./cmd/server` listens; health returns JSON. *(Done; trade/price routes register when `DATABASE_URL` and price-shard config are valid.)*

---

## Phase 2 — Dockerized PostgreSQL

**Goal:** Repeatable local DB.

**Create `docker-compose.yml`:**

- Service `postgres:16` (or 15), port `5432`, volume for data, env `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`.

**Create `DATABASE_URL`:**

- Example: `postgres://user:pass@localhost:5432/portfolio?sslmode=disable`

**Exit criteria:** `docker compose up -d` then `psql` or any client connects.

---

## Phase 3 — Schema migrations (source of truth for storage)

**Goal:** Tables from [LLD Section 5](docs/design/LLD.md) — minimal v1 set.

**Migrations in repo:** `000001_smoke`, `000002_v1_baseline` (core tables), `000003_projection_cursor` (apply cursor), `000004_symbol_returns` (risk inputs).

| Table                  | Purpose                                       |
| ---------------------- | --------------------------------------------- |
| `events`               | Append-only canonical events (JSON payload)   |
| `dlq_events`           | Dead letters                                  |
| `positions_projection` | Current position per `(portfolio_id, symbol)` |
| `prices_projection`    | Last price per symbol                         |
| `projection_cursor`    | Last applied `(event_time, event_id)` per partition |
| `symbol_returns`       | Daily per-symbol returns used by risk         |
| `portfolio_snapshots`  | Optional periodic checkpoints (table only)    |
| `risk_snapshots`       | Optional materialized risk (table only)       |


**Indexes (must-have):**

- `events (portfolio_id, event_time, event_id)` — deterministic replay
- Unique `(portfolio_id, idempotency_key)` on `events` (current schema)

**Down migrations:** `000001_init.down.sql` drops tables in safe order.

**Wire:** Makefile targets `migrate-up` / `migrate-down` calling migrate CLI with `DATABASE_URL`.

**Exit criteria:** Clean `up` on empty DB; `down` then `up` repeatable.

---

## Phase 4 — Domain models and validation

**Goal:** Typed structs matching LLD Section 4; validation before append.

**Create in `internal/domain`:**

- `EventEnvelope`, `TradePayload`, `PricePayload`
- `Side` enum, validation helpers (`quantity > 0`, `price > 0`, symbol regex)
- Domain errors: `ErrPositionUnderflow`, `ErrValidation`, etc.

**Rules (v1):**

- No shorting: reject sell if `qty > current_qty` at **apply** time (not at HTTP validation only—HTTP may not know latest state without read).

**Exit criteria:** Unit tests for validation; table-driven tests for symbol/quantity/price.

---

## Phase 5 — Event repository (append + read stream)

**Goal:** All writes go through one repository.

**Implement `internal/events/store.go`:**

- `AppendEvent(ctx, envelope)` — insert into `events`; return `event_id`
- `FetchEventsSince(ctx, portfolio_id, cursor)` — paginated by `(event_time, event_id)`
- `ExistsByIdempotencyKey(...)` for dedupe

**Transaction boundaries:**

- Append should be single-statement insert; batch replay uses read-only queries.

**Exit criteria:** Integration test with real Postgres (testcontainers or docker compose in CI optional; local docker is fine for v1).

---

## Phase 6 — Ordering + dedupe + apply loop (in-process worker)

**Goal:** Implement LLD Section 6 without external queue.

**Design pattern:**

- HTTP handlers **only append** to `events` (fast path).
- Background **goroutine** per `portfolio_id` (v1 single portfolio OK — still structure as `map[portfolioID]*worker` for clarity).
- Worker loop:
  1. Read new events from DB since last applied cursor (or poll `events` where `applied_watermark` — see note below).

**Two implementation options (pick one in code comments):**

**Option A — Simple v1 (recommended first):**  
No separate `applied` flag. Worker maintains in-memory `last_applied (event_time, event_id)` and on tick re-reads tail, buffers by watermark `W`, sorts, applies.

**Option B — DB cursor table:** **Implemented** — `projection_cursor` updated in the same transaction as projection rows or DLQ+cursor (`migrations/000003`).

**Watermark:** `ORDERING_WATERMARK_MS`; optional `ORDERING_MAX_EVENT_AGE_MS` DLQ at apply.

**Dedupe:** unique `(portfolio_id, idempotency_key)`; duplicate → HTTP 200 + `status: duplicate`.

**Apply path (as implemented):** **Two** transaction types: (1) `ApplyBatch` — trade events only → `positions_projection` + cursor for real `portfolio_id`. (2) `ApplyPriceBatch` — `PriceUpdated` only → `prices_projection` + cursor for **synthetic** price-shard `portfolio_id`. They are **not** merged into one txn across both tables (by design for async ingestion; see LLD §14.1).

**Exit criteria:** Given out-of-order inserts within W, final positions match sorted order; duplicates no-op.

---

## Phase 7 — Portfolio projector + accounting

**Goal:** Deterministic avg-cost logic from LLD Section 7.

**Implement (evolve current code):** logic currently lives in `internal/portfolio/aggregate.go` (apply trades) and `internal/events/postgres.go` (`ApplyBatch`). Prefer extracting pure `ApplyTrade` / `ApplyPrice` if needed for testing.

- Target: `RecalculateUnrealized` using price snapshot once read API exists.
- **Today:** `ApplyBatch` upserts quantity + placeholder `cost_basis`; no separate `projector.go` file yet.

**Invariants:** assert `quantity >= 0` after each apply in tests.

**Exit criteria:** Golden-file or table tests for buy/buy/sell sequences; replay test: re-apply same events → identical DB row.

---

## Phase 8 — Price cache projection

**Status:** **Complete** for projection write path and tests. (`unpriced` in read DTOs is **Phase 9**.)

**Goal:** `prices_projection` updated from `PriceUpdated` on the **price worker** path; cursor advanced with the batch in one txn.

**Consistency:** Cross-table “same moment” snapshot is **not** required. Responses must be **accurate to committed data** in each projection (and mark `unpriced` when portfolio read API exists). See LLD §14.1.

**Implementation:** `internal/events/postgres.go` — `ApplyPriceBatch`; `internal/events/price_worker_pool.go` — partition workers call it after watermark-eligible `PriceUpdated` batches. `as_of` in `prices_projection` is the event’s `event_time`.

**Tests (integration, migrated DB):** `make test-integration` runs  
`TestHTTP_PriceUpdated_MaterializesProjection` (`internal/api/integration_test.go` — HTTP → events → price pool) and  
`TestApplyPrice_ShuffledAppend_LatestInProjection` / `TestApplyPrice_Restart_NoDoubleApply` (`internal/events/integration_test.go` — store append → `pricePartitionWorker`).

**Exit criteria:** Price rows visible in DB after apply (**met**). Missing price → `unpriced` in API DTO (**Phase 9** — `GET /v1/portfolios/:id`).

---

## Phase 9 — HTTP API (Gin) — trades, prices, queries

**Goal:** LLD Section 10 endpoints.

**Done:**
- `POST /v1/trades`, `POST /v1/prices` — nested `trade` / `price` JSON; responses `201`/`200` + `event_id` (see LLD §10.1–10.2).
- Request ID middleware; Gin recovery; structured access logs.

**Remaining:**
- `GET /v1/portfolios/:id` — read projections + prices; `as_of_*`, `unpriced_symbols`, `driving_event_ids`, totals/PnL once §7 is fully materialized.
- Rate limit optional.
- Standardize errors to LLD §12 shape (`error_code`, `request_id`, etc.).

**Exit criteria:** Manual curl: post trade, post price, **then** get portfolio with expected fields (blocked until GET + PnL path).

---

## Phase 10 — Risk engine + endpoint

**Goal:** After each apply batch (or debounced), recompute risk; persist optional `risk_snapshots`.

**Implement `internal/risk`:**

- Exposure, weights, rolling vol.

### v1 choice — returns and rolling vol state

For v1, lock the data path as:

- **Persisted source of truth:** `events` (including `PriceUpdated` payloads).
- **Persisted derived analytics state:** `symbol_returns` table with `(symbol, date, return)` computed when `PriceUpdated` applies.
- **Derived on read:** rolling volatility per symbol from the most recent returns window in `symbol_returns` (for example 20d/60d).

Not chosen for v1:

- Append-only `price_history` table as the primary analytics source.
- `symbol_vol_state` as the only persisted volatility state.
- Replaying full price events on each risk read.

Rationale: this keeps reads fast and deterministic while keeping persisted analytics state easy to inspect and rebuild if formulas change.

### schema changes for this v1 choice

- Add migration `000004_symbol_returns` with table `symbol_returns` keyed by `(symbol, return_date)` and storing the computed daily return value.
- Add a read-friendly index for rolling windows by symbol/date.
- Keep `risk_snapshots` schema as-is: it already matches LLD (`id`, `portfolio_id`, `as_of_event_time`, `as_of_event_id`, `snapshot` JSONB, `created_at`).
- Wire a `risk_snapshots` writer only if we decide v1 needs persisted risk history; otherwise compute risk on demand from projections + `symbol_returns`.

**VaR:** parametric formula from LLD; return `assumptions` JSON block.

**Trigger:** call from worker after successful projection txn; debounce with `time.AfterFunc` per portfolio.

**Exit criteria:** `GET /v1/portfolios/:id/risk` returns stable JSON; values change when prices change.

---

## Phase 11 — Scenario engine

**Goal:** `POST /v1/portfolios/:id/scenarios` — clone state in memory, apply shocks, return deltas; **no DB writes** except optional audit log (skip v1).

**Exit criteria:** Integration test: +10% AAPL shock changes scenario market value deterministically.

---

## Phase 12 — Snapshots (checkpoint)

**Goal:** Periodic write to `portfolio_snapshots` every N events or T seconds for faster restart.

**On startup:** load latest snapshot + replay events after `as_of_event_id`.

**Exit criteria:** Kill process, restart, state matches continuous run (within ordering policy).

---

## Phase 13 — AI layer (OpenAI)

**Goal:** LLD Section 11.

**Implement:**

- `POST /v1/portfolios/:id/insights/explain` assembles DTO (portfolio + risk + last events JSON)
- `internal/ai/prompt.go` — system prompt: “only use provided JSON numbers; cite field names”
- `internal/ai/validator.go` — regex or simple checks: tickers from input appear; ban phrases like “I updated your portfolio”

**Secrets:** `OPENAI_API_KEY` from env; never log prompt with secrets.

**Exit criteria:** With mock HTTP client in tests, validator rejects bad outputs.

---

## Phase 14 — Observability

**Goal:** Production hygiene.

- Structured logs: `portfolio_id`, `event_id`, `request_id`, `latency_ms`
- Metrics (optional): `events_appended_total`, `dlq_events_total`, `projection_lag_seconds`
- `GET /metrics` if Prometheus enabled
- Env toggle: `PROMETHEUS_ENABLED=true` to mount `/metrics`

---

## Phase 15 — Dockerfile for app

**Goal:** Multi-stage build: compile static binary, minimal image (distroless or alpine).

**Compose:** optional `app` service depending on `postgres`, passing `DATABASE_URL`.

**Repository:** `Dockerfile` exists; wire `app` in `docker-compose.yml` if you want one-command full stack.

---

## Phase 16 — Testing strategy (minimum bar)

**Must have:**

- Unit: accounting, ordering comparator, VaR math
- Integration: postgres + migrate + full HTTP flow
- Replay equivalence test

**Optional:** `testing/quick` or `gopter` for properties.

---

## Suggested order summary (checklist)

1. Phase 0–1: skeleton + health
2. Phase 2–3: Postgres + migrations
3. Phase 4–5: domain + event store
4. Phase 6–8: worker + portfolio + prices
5. Phase 9: HTTP API
6. Phase 10–11: risk + scenarios
7. Phase 12: snapshots + recovery
8. Phase 13–15: AI + observability + Docker app
9. Phase 16: harden tests

---

## Mermaid: runtime data path (v1)

```mermaid
flowchart LR
  Client[Client] --> Gin[GinAPI]
  Gin --> EventAppend[AppendToEventsTable]
  EventAppend --> PG[(Postgres)]
  TradeW[TradeWorkerPool] --> PG
  PriceW[PriceWorkerPool] --> PG
  TradeW --> PosProj[positions_projection txn]
  PriceW --> PxProj[prices_projection txn]
  PosProj --> PG
  PxProj --> PG
```

*(Read path `GET /portfolio` not wired yet.)*



---

## Notes on Gin vs Chi

- **Gin:** faster to scaffold; global router patterns familiar.
- **Chi:** stdlib `http.Handler` style; finer-grained middleware trees.

Pick one before Phase 9 and do not mix routers.