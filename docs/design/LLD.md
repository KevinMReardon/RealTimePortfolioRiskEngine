# Low-Level Design (LLD)
## Real-Time Portfolio & Risk Engine with AI Insights

## 1. Purpose

This document translates product requirements from `docs/design/PRD.md` and system architecture from `docs/design/HLD.md` into implementation-ready design details for v1.

Goals of this LLD:
- Define concrete module interfaces and contracts.
- Define data schemas, API payloads, and processing algorithms.
- Define deterministic ordering/idempotency behavior.
- Define test strategy and operational checks.

---

## 2. Scope (v1)

Included:
- Trade ingestion and price ingestion.
- Append-only event store.
- Ordering, dedupe, and dead-letter handling.
- Portfolio projection (weighted average cost).
- Risk metrics (exposure, concentration, volatility, parametric VaR 95% 1-day).
- Scenario simulation (shock-based, side-effect free).
- AI insight orchestration (read-only, structured JSON only).
- Explainability and lineage fields on API responses.

Excluded from v1:
- FIFO accounting.
- Historical VaR.
- Multi-asset derivatives support.
- AI write access or autonomous actions.

---

## 2.1 Implementation status (this repository)

The **reference implementation** is a Go monolith (`github.com/KevinMReardon/realtime-portfolio-risk`). The following reflects the **current** tree, not a future target layout.

**Implemented today**
- Runnable server: `cmd/server` — PostgreSQL pool, starts **two** worker pools (trade portfolios + price-stream partitions), Gin router.
- HTTP: `GET /health`; `POST /v1/trades`; `POST /v1/prices` (`internal/api`). Request ID + structured request logging (`internal/observability`).
- Ingestion validates envelopes/payloads then appends (`internal/ingestion`, `internal/domain`).
- Event store + apply: `internal/events` (`PostgresStore`) — append, fetch since cursor, list portfolios, apply batches, DLQ persist.
- Schema: `migrations/` — `events`, `dlq_events`, `positions_projection`, `prices_projection`, `projection_cursor`, optional `portfolio_snapshots` / `risk_snapshots` (event + position/price projection writers wired; trade apply may append `portfolio_snapshots` when `SNAPSHOT_ENABLED` and `SNAPSHOT_EVERY_N_EVENTS` / `SNAPSHOT_MIN_INTERVAL_SEC` are set; `LoadLatestPortfolioSnapshot` reads latest row + hydrates aggregate).
- Ordering: watermark `W` (`ORDERING_WATERMARK_MS`); optional wall-clock DLQ (`ORDERING_MAX_EVENT_AGE_MS`). Persisted cursor (**Option B**) in the same transaction as projection updates or DLQ+cursor advance.
- **Dual apply path:** trade events update `positions_projection` + `projection_cursor` for the **real** `portfolio_id`. `PriceUpdated` events are stored under **synthetic** `portfolio_id` shards (derived from symbol hash); a separate worker pool applies `prices_projection` + cursor per shard. These are **not** one PostgreSQL transaction across both tables.
- Positions apply uses `internal/domain` quantity rules (long-only, apply-time SELL guard) and `internal/portfolio.Aggregate`; DB row includes `cost_basis` but apply currently persists **`0` as placeholder** (weighted average cost / realized PnL from §7 are not yet fully materialized in projection rows).

**Not implemented yet (LLD still target)**
- `GET /v1/portfolios/{id}` and read-model DTOs (`as_of_*`, `unpriced_symbols`, lineage).
- Full §7 accounting in projections (average cost, realized/unrealized PnL, market value columns as in §5).
- Risk, scenario, and AI endpoints; snapshot checkpoint/restart path; Prometheus metrics; idempotency **payload-hash conflict** → DLQ (current DB dedupe is `(portfolio_id, idempotency_key)` only).

**Cross-projection reads (contract)**  
Ingestion is asynchronous: trade and price streams commit independently. Any future portfolio API must be **accurate to committed state in each projection**—not a single fused snapshot—unless a later design merges applies. See §14.1.

---

## 3. Code module boundaries

Go package layout (reference implementation):

```text
cmd/server/                 # wiring: config, DB, workers, HTTP
internal/
  api/                      # Gin routes, middleware, HTTP ↔ domain
  config/                   # env-based config (watermark, workers, price shards)
  domain/                   # envelopes, payloads, validation, Positions (quantities)
  events/                   # Postgres store, cursors, trade/price worker pools, apply
  ingestion/                # validate + append service
  observability/            # Zap logger
  portfolio/                # Aggregate (apply trades in memory for projector)
  pricing/                  # stub (projection writes live in events/postgres)
  risk/                     # stub
  scenario/                 # stub
  ai/                       # stub
migrations/                 # golang-migrate SQL
```

Rules:
- `portfolio` and `risk` remain deterministic and pure for core calculations.
- `ai` layer depends on API DTOs, never directly on store internals.
- `events` module owns ordering and idempotency enforcement.

---

## 4. Event contracts

## 4.1 Canonical event envelope

```json
{
  "event_id": "uuid",
  "event_type": "TradeExecuted | PriceUpdated",
  "event_time": "2026-03-19T14:05:30.123Z",
  "processing_time": "2026-03-19T14:05:30.207Z",
  "source": "trade_api | market_feed_sim",
  "portfolio_id": "string",
  "idempotency_key": "string",
  "payload": {}
}
```

Constraints:
- `event_id` must be globally unique.
- `event_time` required and parseable ISO-8601 UTC.
- `portfolio_id` required for partition ordering.
- `idempotency_key` required for ingestion endpoints.

## 4.2 TradeExecuted payload

```json
{
  "trade_id": "string",
  "symbol": "AAPL",
  "side": "BUY | SELL",
  "quantity": 10.0,
  "price": 180.25,
  "currency": "USD"
}
```

Validation:
- `quantity > 0`, `price > 0`.
- `symbol` must match symbol regex and configured universe policy.
- v1 no-shorting: SELL cannot reduce quantity below zero at apply time.

## 4.3 PriceUpdated payload

```json
{
  "symbol": "AAPL",
  "price": 181.75,
  "currency": "USD",
  "source_sequence": 102938
}
```

Validation:
- `price > 0`.
- `symbol` required.

---

## 5. Storage design

## 5.1 Tables / collections

### `events`
Persisted columns (see `migrations/000002_v1_baseline.up.sql`):
- `event_id` (PK, UUID)
- `portfolio_id` (UUID) — real portfolio for trades; **synthetic shard UUID** for `PriceUpdated` (see §6)
- `event_time` (`timestamptz`)
- `event_type` (text)
- `idempotency_key` (text)
- `source` (text)
- `payload` (JSONB)
- `created_at` (`timestamptz`, server default)

Note: `processing_time` and other envelope fields from §4.1 exist on the in-memory / API envelope but are **not** separate columns on `events` in the current schema.

Indexes:
- `(portfolio_id, event_time, event_id)` for deterministic replay scans.
- **Unique** `(portfolio_id, idempotency_key)` for ingest dedupe (not `(source, idempotency_key)` in DB).

### `projection_cursor`
- `portfolio_id` (PK, UUID) — same key space as `events.portfolio_id` (trade portfolio or price shard)
- `last_event_time`, `last_event_id` — last **applied** ordering key
- `updated_at`

Written in the **same transaction** as the matching projection batch or DLQ+cursor advance (`migrations/000003_projection_cursor.up.sql`).

### `dlq_events`
Current schema (simpler than the conceptual model above):
- `id` (bigserial PK)
- `original_event_id` (UUID, nullable)
- `portfolio_id` (UUID, nullable)
- `failed_at`, `error_message`, `payload` (JSONB), `metadata` (JSONB)

### `positions_projection`
- `portfolio_id`, `symbol` (PK pair)
- `quantity`, `cost_basis` (`numeric(20,8)`) — **`cost_basis` not yet average cost; placeholder `0` in apply path**
- `updated_at`

Target LLD fields (`average_cost`, PnL columns, `last_event_id`, etc.) are **not** present as columns yet.

### `prices_projection`
- `symbol` (PK)
- `price` (`numeric(20,8)`)
- `as_of` (`timestamptz`) — taken from the applied event’s `event_time`
- `updated_at`

No `as_of_event_id` column in DB; lineage can be reconstructed from `events` if needed.

### `portfolio_snapshots`
Persisted columns (baseline `migrations/000002_v1_baseline.up.sql`):
- `id` (`bigserial`, PK)
- `portfolio_id` (`uuid`, NOT NULL)
- `as_of_event_time` (`timestamptz`, NOT NULL), `as_of_event_id` (`uuid`, NOT NULL) — apply cursor at checkpoint (see prose below).
- `snapshot` (`jsonb`, NOT NULL)
- `created_at` (`timestamptz`, NOT NULL, default `now()`)

Unique index (`migrations/000008_portfolio_snapshots_checkpoint_unique.up.sql`): `uq_portfolio_snapshots_checkpoint` on `(portfolio_id, as_of_event_time, as_of_event_id)` — enforces at most one row per apply cursor (idempotent snapshot writes) and supports “latest checkpoint per portfolio” via backward btree scan, e.g. `ORDER BY as_of_event_time DESC, as_of_event_id DESC LIMIT 1`. Do not use `created_at` alone as the replay lineage key; row insertion time can differ from `as_of_event_time`. (Migration `000007` introduced a non-unique index; `000008` replaces it with this unique index.)

**Store:** `PostgresStore.InsertPortfolioSnapshot` appends a row; duplicate checkpoints return `Inserted: false` (`ON CONFLICT DO NOTHING`). `PostgresStore.LoadLatestPortfolioSnapshot` returns the newest row by `(as_of_event_time, as_of_event_id)` and hydrates `rtpre_trade_positions_v1` into a single-partition `portfolio.Aggregate` (plus raw JSON and `created_at`).

**Trade worker (checkpoint):** After each successful trade `ApplyBatch`, the partition worker increments an applied-event counter since the last successful snapshot and checks wall time since the last successful snapshot insert. If `SNAPSHOT_ENABLED` is true and (`SNAPSHOT_EVERY_N_EVENTS` > 0 and the counter ≥ N, **or** `SNAPSHOT_MIN_INTERVAL_SEC` > 0 and ≥ T seconds since the last successful insert), it serializes the aggregate (`rtpre_trade_positions_v1`) and **reloads `projection_cursor`**: it inserts only when that row’s `(last_event_time, last_event_id)` **exactly matches** the batch’s last envelope (so snapshot `as_of_*` is never ahead of or divergent from committed cursor). Marshal, insert, reload, or mismatch failures log and skip; counters are not reset so a later batch retries. Snapshots are best-effort accelerators; `projection_cursor` + event replay remain source of truth. Set `SNAPSHOT_ENABLED=false` (e.g. in tests) to disable writes regardless of N/T; with enabled true, both N and T zero disables snapshots. Legacy envs `PORTFOLIO_SNAPSHOT_MIN_EVENTS` / `PORTFOLIO_SNAPSHOT_INTERVAL_SEC` are still read if the new names are unset. Counter resets after a successful insert (including idempotent no-op).

**Scope (trade partition only):** `portfolio_id` here is a **real** customer portfolio (the trade event stream). `PriceUpdated` rows live under **synthetic** shard `portfolio_id`s (§6); their checkpoints belong to price-shard cursor + `prices_projection`, not this JSON. A cold-started trade worker must be able to rebuild `internal/portfolio.Aggregate` / `domain.Positions` for one portfolio from one row plus replay.

**Ordering key (“applied through”):** `as_of_event_time` and `as_of_event_id` are the **authoritative** cursor for recovery. They must equal the last envelope in a successfully committed trade `ApplyBatch` for this `portfolio_id`—the same tuple persisted as `(last_event_time, last_event_id)` in `projection_cursor` for that portfolio at the instant the snapshot was taken. Ordering is always `(event_time ASC, event_id ASC)` (§6). **Replay** after restore: load all events strictly **after** that tuple, i.e. `(event_time > as_of_event_time) OR (event_time = as_of_event_time AND event_id > as_of_event_id)` (same predicate as `FetchAfter` in the reference store).

**Consistency with in-memory apply:** The `snapshot` JSON must materialize exactly the per-symbol lots the trade worker’s aggregate holds **immediately after** applying the event identified by `(as_of_event_time, as_of_event_id)`—equivalent to hydrating from `positions_projection` for that portfolio when the cursor matches those columns: `quantity`, weighted-average unit cost (DB `cost_basis`), and cumulative `realized_pnl`, including **flat** lots (`quantity = 0`, non-zero realized) retained per §7 until cleared by a later trade.

**`snapshot` JSONB contract (`rtpre_trade_positions_v1`):**

- Top-level object:
  - `format` (string, required): literal `"rtpre_trade_positions_v1"` for versioning.
  - `positions` (array, required): one element per symbol that has non-zero quantity **or** non-zero realized PnL after the as-of event (the full `domain.Positions` map; omit symbols with no lot).
- Each `positions[]` element:
  - `symbol` (string, required)
  - `quantity` (string, required): decimal string, scale aligned with `numeric(20,8)` (no JSON floats).
  - `average_cost` (string, required): weighted average **unit** cost for remaining shares; same semantics as `PositionLot.AverageCost` / `positions_projection.cost_basis`. When `quantity` is zero, use `"0"` (matches apply logic after a full sell).
  - `realized_pnl` (string, required): cumulative realized PnL for the symbol; same as `PositionLot.RealizedPnL` / `positions_projection.realized_pnl`.

Writers should sort `positions` by `symbol` ascending for deterministic blobs. Readers must reject unknown `format` values or fail closed.

Example:

```json
{
  "format": "rtpre_trade_positions_v1",
  "positions": [
    {
      "symbol": "AAPL",
      "quantity": "100.00000000",
      "average_cost": "150.25000000",
      "realized_pnl": "0.00000000"
    },
    {
      "symbol": "TSLA",
      "quantity": "0.00000000",
      "average_cost": "0.00000000",
      "realized_pnl": "1234.56000000"
    }
  ]
}
```

**Recovery (normative):** hydrate positions from `snapshot`, set apply cursor from **row** `as_of_event_time` / `as_of_event_id` (not only from JSON), then replay subsequent trade events for that `portfolio_id`. Optional: duplicate the as-of tuple inside JSON for portable exports; if present, it must match the row columns. **Trade worker `rebuild`:** loads latest snapshot; uses it when `as_of` equals `projection_cursor` or when the cursor row is absent (cold partition), else ignores a stale snapshot (`as_of` before cursor) or an anomalous ahead-of-cursor snapshot and hydrates from `positions_projection` + DB cursor as before.

**Restart invariant:** Restart = latest snapshot + replay tail of events; equivalent to never stopping, modulo ordering policy (§6; `internal/events/trade_portfolio_apply.go`: `partitionWorker.rebuild`, `pumpAfterCursor`).

### `risk_snapshots`
- `id` (bigserial PK), `portfolio_id`, `as_of_event_time`, `as_of_event_id`, `snapshot` (JSONB), `created_at`

Table exists; **no application writer** in the current codebase.

---

## 6. Ordering and idempotency engine

Partition key:
- **Trades:** `portfolio_id` = caller-supplied portfolio UUID (strict ordering and single-threaded apply per portfolio among trade workers).
- **Prices:** `portfolio_id` = one of N **synthetic** UUIDs (`PRICE_STREAM_PORTFOLIO_ID` namespace + `PRICE_STREAM_SHARD_COUNT`); chosen deterministically from `symbol` so price events shard the event stream. A **separate** worker pool applies only `PriceUpdated` for those partitions.

Ordering key:
- `(event_time ASC, event_id ASC)`.

Watermark behavior:
- Track `max_event_time_seen` per partition.
- Event is eligible for apply when:
  - `event_time <= max_event_time_seen - W` where `W` is configurable (default: 2 seconds).

Late policy:
- If older than watermark tolerance and cannot be safely inserted:
  - send to `dlq_events` with reason `LATE_EVENT_BEYOND_WATERMARK`.

Idempotency behavior (current DB):
- Unique key `(portfolio_id, idempotency_key)`. Second submit with the same pair **does not insert**; API returns the existing `event_id` as duplicate.
- **Payload-hash conflict** detection (same key, different body → DLQ) is **not** implemented yet; future work if required.

Pseudo-flow:

```text
ingest -> validate -> canonicalize -> write events
      -> ordering_buffer(partition)
      -> dedupe_check
      -> apply in sorted order
      -> update projections
      -> emit metrics
```

---

## 7. Portfolio projection logic (weighted average cost)

**Target logic (v1 product):** weighted average cost and mark-to-market as below.

**Current implementation:** apply path persists **quantities** (and placeholder `cost_basis = 0`). `PriceUpdated` does not update `positions_projection` (prices live in `prices_projection` only). Full §7 PnL fields will align once portfolio read API and projection columns are extended.

Inputs:
- Ordered `TradeExecuted` and `PriceUpdated` events.

State per symbol (target):
- `quantity`, `average_cost`, `realized_pnl`, `last_price`, `unrealized_pnl`, `market_value`.

Trade apply logic:

For `BUY(q, p)`:
- `new_qty = old_qty + q`
- `new_avg = ((old_qty * old_avg) + (q * p)) / new_qty`
- `realized_pnl` unchanged

For `SELL(q, p)`:
- Preconditions: `old_qty >= q` (no shorting v1)
- `realized_pnl += (p - old_avg) * q`
- `new_qty = old_qty - q`
- `new_avg = old_avg` when `new_qty > 0`, else `0`

Price apply logic:
- `last_price = p`
- `unrealized_pnl = (last_price - average_cost) * quantity`
- `market_value = last_price * quantity`

Portfolio totals:
- `total_market_value = sum(symbol.market_value for priced symbols)`
- `total_unrealized_pnl = sum(symbol.unrealized_pnl for priced symbols)`
- `total_realized_pnl = sum(symbol.realized_pnl)`

Missing price behavior:
- If held symbol has no price, mark as `unpriced`.
- Exclude from market value and VaR inputs.
- Include in `unpriced_symbols` response metadata.

---

## 8. Risk engine details

## 8.1 Trigger policy

Recompute risk on:
- Every `TradeExecuted`.
- `PriceUpdated` affecting symbols with open quantity.

Optional debounce:
- Coalesce recomputes in 100-500ms windows under burst load.

## 8.2 Exposure and concentration

- Symbol exposure: `abs(quantity * last_price)` (or signed notional; choose one and keep consistent in API).
- Portfolio exposure: sum of symbol exposures.
- Weight: `symbol_market_value / total_market_value` for priced symbols.
- Concentration: top-N weights and Herfindahl-style score (optional).

## 8.3 Volatility input

v1 default:
- Rolling window daily returns per symbol, `window_n = 60`.
- Annualized volatility converted to 1-day:
  - `sigma_1d = sigma_annual / sqrt(252)`.

## 8.4 Parametric VaR (95%, 1-day)

Per-portfolio approximation (v1):
- `VaR_95_1d = total_market_value * sigma_1d_portfolio * Z`
- `Z = 1.645` (one-tailed, 95% confidence).

Required output assumptions:
- `confidence = 0.95`
- `horizon_days = 1`
- `z_score = 1.645`
- `volatility_convention = annual_to_daily_sqrt_252`
- `model = parametric_normal`

---

## 9. Scenario engine details

Input:

```json
{
  "portfolio_id": "p1",
  "shocks": [
    { "symbol": "AAPL", "type": "PCT", "value": -0.05 }
  ]
}
```

Execution:
- Clone current projection state in-memory.
- Apply shock-adjusted prices to scenario-local copy.
- Recompute market value, unrealized PnL, and risk outputs.
- Return result with `base_as_of_event_id` and `base_as_of_event_time`.

Guarantee:
- No mutations to production projections or event store.

---

## 10. API contract (v1)

## 10.1 Trade ingestion

`POST /v1/trades`

**Implemented request shape** (`portfolio_id` must be a UUID string; must not equal a reserved price-shard id):

```json
{
  "portfolio_id": "550e8400-e29b-41d4-a716-446655440000",
  "idempotency_key": "client-req-123",
  "source": "trade_api",
  "event_time": "2026-03-19T14:05:30.123Z",
  "trade": {
    "trade_id": "t-1001",
    "symbol": "AAPL",
    "side": "BUY",
    "quantity": "10",
    "price": "180.25",
    "currency": "USD"
  }
}
```

`event_time` optional (defaults to server UTC). Trade numeric fields use string decimals in JSON (domain validation).

**Implemented response:**
- `201` with `{ "event_id": "...", "status": "created" }` on first insert.
- `200` with `{ "event_id": "...", "status": "duplicate" }` on idempotent replay.

Errors: JSON `{ "error": "..." }` (not yet full §12 shape everywhere).

## 10.2 Price ingestion

`POST /v1/prices`

**No `portfolio_id` in body** — server assigns the synthetic shard `portfolio_id` from `symbol`. Request:

```json
{
  "idempotency_key": "md-abc",
  "source": "market_feed_sim",
  "event_time": "2026-03-19T14:05:30.123Z",
  "price": {
    "symbol": "AAPL",
    "price": "181.75",
    "currency": "USD",
    "source_sequence": 102938
  }
}
```

Response: same `201`/`200` + `event_id` / `status` pattern as trades.

## 10.3 Portfolio query

`GET /v1/portfolios/{portfolio_id}`

**Not implemented in the reference service yet** (target shape below).

Response shape:
- positions list
- totals (`market_value`, `realized_pnl`, `unrealized_pnl`)
- `unpriced_symbols`
- lineage: `driving_event_ids`
- as-of fields: `as_of_event_id`, `as_of_event_time`, `as_of_processing_time`

## 10.4 Risk query

`GET /v1/portfolios/{portfolio_id}/risk`

Response shape:
- exposure, concentration, volatility, `var_95_1d`
- assumptions block
- lineage and as-of fields

## 10.5 Scenario run

`POST /v1/portfolios/{portfolio_id}/scenarios`

Response:
- base snapshot metadata
- shocked outputs
- delta vs base

## 10.6 AI explain

`POST /v1/portfolios/{portfolio_id}/insights/explain`

Behavior:
- API assembles structured JSON context.
- AI orchestrator sends prompt + JSON to LLM.
- response validator ensures:
  - symbols referenced exist in input
  - numeric claims map to provided metrics
  - no forbidden actions in output

---

## 11. AI orchestration constraints

Input to AI:
- Portfolio snapshot DTO
- Risk snapshot DTO
- Recent event summary DTO
- Optional scenario result DTO

Guardrails:
- No raw DB/tool access.
- If required fields missing, return `INSUFFICIENT_DATA`.
- Output includes `used_metrics` list for auditability.

---

## 12. Error model

Standard error response:

```json
{
  "error_code": "VALIDATION_ERROR",
  "message": "quantity must be > 0",
  "details": {},
  "request_id": "uuid"
}
```

Core error codes:
- `VALIDATION_ERROR`
- `IDEMPOTENCY_CONFLICT`
- `LATE_EVENT_BEYOND_WATERMARK`
- `POSITION_UNDERFLOW`
- `UNPRICED_POSITIONS_PRESENT`
- `INSUFFICIENT_DATA`
- `RATE_LIMITED` (HTTP 429 when per-IP HTTP rate limiting applies)

---

## 13. Observability and operations

Metrics:
- ingestion rate, reject rate
- ordering buffer depth
- event lag (`processing_time - event_time`)
- projection lag by portfolio
- DLQ count and reason distribution
- replay duration and replay drift count

Logs (structured):
- `request_id`, `event_id`, `portfolio_id`, `reason_code`.

Tracing:
- Span from ingest -> event append -> apply -> projection update -> API serve.

---

## 14. Invariants and consistency checks

Must hold:
- Idempotent processing.
- Non-negative position quantity (v1 no shorting).
- Portfolio totals equal sum of priced position fields.
- Live state equals replayed state for same ordered event set.

### 14.1 Cross-projection consistency (reads)

- **Per partition:** projection row updates and `projection_cursor` for that `portfolio_id` commit together (trade batch, price batch, or DLQ path).
- **Across partitions:** `positions_projection` (trade portfolios) and `prices_projection` (global symbol marks) advance in **separate transactions** and schedules. That matches asynchronous ingestion: a read may see **new positions with an older price** or vice versa until both applies complete.
- **API contract:** when portfolio reads ship, responses must be **accurate for whatever is currently committed** in each store (and expose `unpriced` / separate as-of metadata as needed)—not an implied single global snapshot unless a future design merges applies.

Checks:
- periodic invariant job per partition
- replay-based verification job (sampled or full)

---

## 15. Test strategy

## 15.1 Unit tests
- avg-cost accounting math (buy/sell sequences)
- PnL realization and mark-to-market logic
- ordering comparator and watermark logic
- idempotency conflict detection
- VaR formula and assumptions payload

## 15.2 Integration tests
- ingest trade + price -> projection update
- out-of-order events within W reordered correctly
- beyond-W events routed to DLQ
- replay rebuild equals live projection
- scenario execution is side-effect free

## 15.3 Contract tests
- API response shape includes as-of and lineage fields
- AI validator blocks non-grounded output

## 15.4 Property tests (recommended)
- Determinism under repeated event streams
- Commutativity only where allowed by ordering policy

---

## 16. Configuration

**Implemented (environment variables, see `internal/config`):**
- `DATABASE_URL` (required)
- `PORT` (default `8080`)
- `ORDERING_WATERMARK_MS` (default `2000`; `0` disables buffer)
- `ORDERING_MAX_EVENT_AGE_MS` (optional; `0` off — late wall-clock DLQ at apply)
- `APPLY_WORKER_TICK_MS` (default `500`)
- `APPLY_WORKER_COUNT` (default `8`)
- `PRICE_STREAM_PORTFOLIO_ID` (namespace UUID for shard derivation)
- `PRICE_STREAM_SHARD_COUNT` (default `16`)
- `PRICE_APPLY_WORKER_COUNT` (default `16`)
- `SHUTDOWN_TIMEOUT_SECONDS` (default `10`)

**Planned / LLD-only (not wired in reference binary):**
- `RISK_RECOMPUTE_DEBOUNCE_MS`, `VOL_WINDOW_DAYS`, `VAR_*`, `NO_SHORTING_ENABLED`, debug config endpoint.

---

## 17. Rollout plan (implementation order)

1. Event models + ingestion validation + event store. *(Done in reference repo.)*
2. Ordering/dedupe/watermark + DLQ path. *(Done; payload-hash conflict not yet.)*
3. Portfolio projector + snapshots + replay. *(Partial: cursor + replay; avg cost/PnL + snapshot recovery pending.)*
4. Risk engine (exposure, concentration, volatility, VaR).
5. Portfolio/risk/scenario APIs with as-of and lineage fields. *(Ingest only; GET portfolio pending.)*
6. AI orchestration and grounded-response validation.
7. Observability, invariant jobs, and reliability hardening. *(Structured logs + request ID; metrics/invariant jobs pending.)*

---

## 18. Traceability matrix

PRD/HLD requirement mapping:
- Accounting model -> Section 7.
- Ordering + out-of-order policy -> Section 6.
- Risk assumptions (95%, 1-day, parametric) -> Section 8.
- AI read-only + grounding -> Sections 10 and 11.
- Lineage/explainability -> Sections 10, 13, and 14.
- Replay/reconciliation -> Sections 5, 6, 14, and 15.

This LLD is the implementation contract for v1 and should be updated alongside any requirement changes in `docs/design/PRD.md` and `docs/design/HLD.md`.
