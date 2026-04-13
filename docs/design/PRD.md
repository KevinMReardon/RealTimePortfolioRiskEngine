# PRD v1.0 — Real-Time Portfolio & Risk Engine with AI Insights

## 1) Product overview

**Product name:** Real-Time Portfolio & Risk Engine with AI Insights

**One-liner:** An event-driven system that ingests trades and market data, maintains reconstructable portfolio state, computes **deterministic** risk metrics with explicit assumptions, and provides **AI explanations and scenario interpretation** grounded only in structured system outputs.

**Core question answered:** *How is my portfolio performing, how risky is it, and what could happen next?*

---

## 2) Problem statement

Stakeholders need **immediate, accurate, explainable** answers for performance, risk drivers, and scenario impact. The product must demonstrate **financial correctness**, **real-time reactivity**, and **production-aware AI** (structured I/O, no authority over state).

---

## 3) Goals and non-goals

**Goals**

- Correctness-first portfolio accounting from an **append-only event log** (replayable).
- Real-time reaction to **trades** and **price updates**, with an **explicit ordering strategy** for out-of-order data.
- Deterministic risk metrics with **documented assumptions** (horizon, confidence, method).
- **Data lineage:** every derived metric traceable to source events and intermediate steps.
- AI as **interpreter only**: structured input, grounded output referencing real symbols and numbers.

**Non-goals**

- Price prediction / alpha; AI modifying portfolio or writing events.
- Premature microservices; start with clear module boundaries.
- HFT-grade latency targets (demo-grade real-time is sufficient).

---

## 4) Accounting model (explicit) — v1 locked

**v1 decision — position tracking:** **Weighted average cost (average cost)** per symbol.

**Definitions**

- **Position quantity:** net shares held per symbol after applying signed trade quantities.
- **Average cost (unrealized basis):** after each **buy**, update weighted average:
  - New avg = (prior_qty × prior_avg + buy_qty × buy_price) / (prior_qty + buy_qty).
- **Unrealized PnL (mark-to-market):**  
  `(current_market_price - average_cost) × quantity`  
  (only when quantity ≠ 0).

**Realized vs unrealized**

- **Unrealized PnL:** updates on every **PriceUpdated** (and when position/qty or avg cost changes).
- **Realized PnL:** recognized on **sell execution** (partial or full):
  - For quantity sold `q_sold` at execution price `P_sell`:  
    `realized += (P_sell - average_cost_at_time_of_sell) × q_sold`
  - After a sell, **reduce quantity**; **average cost is unchanged** under average-cost method for remaining shares (standard simplified equity model for v1).

**When PnL is realized**

- At the moment a **valid sell trade event** is applied (after ordering/idempotency rules), not when price moves.

**Partial fills (optional for v1)**

- **Out of scope for MVP** unless modeled as multiple **TradeExecuted** events with the same logical `order_id` and strict idempotency on `(order_id, fill_seq)`; document if added later.

**Extensibility (signals mature design)**

- Internal interfaces should allow a future **pluggable accounting strategy** (average cost → FIFO) without rewriting ingestion or the event model.

---

## 5) Event ordering & time semantics — v1 specified

**Concepts**

- **Event-time:** timestamp assigned by the market/trade source (or client), carried on the event payload.
- **Processing-time:** when the system receives/processes the event.

**v1 strategy (choose one primary; document in implementation)**

1. **Ordering buffer (watermark)** — default recommendation for v1 clarity:
   - Maintain a per-stream or global buffer; apply events only when `event_time <= (max_observed_event_time - W)` where **W = 1–5 seconds** (configurable).
   - Late events within W are **re-sorted** by `(event_time, event_id)` and applied deterministically.
2. **Reconciliation pass** — optional enhancement:
   - Fast path applies events in arrival order; a periodic **recompute-from-event-log** job (or triggered checkpoint) verifies invariants and repairs drift if detected.

**Requirements**

- **Deterministic tie-break:** total order = `(event_time ASC, event_id ASC)` (or `(event_time, event_type_priority, event_id)` if trade vs price conflicts need a rule — document the rule).
- **Idempotency:** duplicate ingestion must not double-apply (`event_id` or business key).
- **Out-of-order beyond W:** either reject to dead-letter with reason, or apply with explicit **correction event**; **must be documented** (no silent wrong state).

---

## 6) Risk assumptions — v1 locked

**Value at Risk (VaR) — v1 method:** **Parametric (normal) VaR**

- **Horizon:** **1 trading day**
- **Confidence:** **95%** → **Z = 1.645** (one-tailed loss; document one-tailed vs two-tailed in API/docs)
- **Inputs:** portfolio mark-to-market value and a defined **volatility estimate** (e.g., annualized vol scaled to 1-day: `σ_1d = σ_annual / sqrt(252)` unless you standardize on a different convention — **must be documented in API**)

**Other risk metrics (v1)**

- Exposure per symbol and total notional/market value.
- Concentration (e.g., top-N weights).
- Volatility estimate (simple rolling window on returns — window length documented).

**Future extension (resume signal)**

- Add **historical VaR** as a second method and expose **comparison** of outputs (same horizon/confidence).

---

## 7) AI layer — enforceable constraints (v1)

**Input contract**

- AI service accepts **structured JSON only** (portfolio snapshot, risk snapshot, recent event summaries, scenario results). **No raw DB access** and no arbitrary SQL.

**Output contract (grounding)**

- Responses **must reference** at least:
  - **Symbols** (tickers)
  - **Numeric metrics** actually present in input JSON (e.g., PnL, exposure, VaR, weights)
- Prefer a **citation pattern**: quote metric names and values exactly as provided (e.g., `"unrealized_pnl_usd": 1234.56`).

**Authority**

- AI **must not** compute authoritative financial metrics; **must not** mutate state or emit trades.

**Failure modes**

- If required numbers are missing, AI returns **insufficient data** rather than inventing figures.

---

## 8) Data lineage & explainability (explicit requirement)

For every **derived metric** exposed via API (PnL, position, exposure, VaR, scenario outputs):

- **Traceability:** identify **source event IDs** (trades/prices) and/or **time window** used.
- **Intermediate artifacts:** where practical, expose a **breakdown** (per-symbol contribution to PnL delta, inputs to VaR).
- **Replay guarantee:** given the same ordered event log, derived metrics are **reproducible**.

Minimum v1 deliverable: API fields or companion “explain” object listing **contributing symbols** and **last N driving events** for dashboard/AI consumption.

---

## 9) Scope by phase (aligned with build strategy)

| Phase | Goal | Included |
|-------|------|----------|
| 1 | Deterministic core | Events, portfolio engine (avg cost), tests, replay |
| 2 | Real-time behavior | Streaming/simulated prices, live API updates |
| 3 | Risk layer | Exposure, concentration, vol, **parametric VaR (95%, 1d)** |
| 4 | AI layer | Structured JSON in/out, grounded explanations |
| 5 | Simulation & replay | Scenarios, checkpoint/replay demos |

---

## 10) Functional requirements (summary)

- **Ingestion:** external data → canonical internal events (`TradeExecuted`, `PriceUpdated`, …).
- **Event store:** append-only, replayable, idempotent.
- **Portfolio engine:** positions, avg cost, unrealized/realized PnL, NAV — per **Accounting Model**.
- **Risk engine:** metrics per **Risk assumptions**; assumptions exposed in API.
- **Scenario engine:** deterministic shocks → projected PnL/risk-style outputs.
- **API:** clean contracts for portfolio, risk, scenarios, lineage/explain payloads.
- **AI service:** read-only; **AI constraints** enforced.

---

## 11) Non-functional requirements

- **Correctness > cleverness;** invariant checks (e.g., qty never negative, reconciliation hooks).
- **Performance:** demo real-time target (e.g., sub-second UI/API refresh) — non-binding vs correctness.
- **Observability:** correlation IDs from ingestion through derived metrics.

---

## 12) Success criteria (concrete)

- Portfolio reconstructable from events; **no double counting**.
- Out-of-order policy **documented and tested** (buffer width W, tie-break, idempotency).
- VaR reproducible with stated **σ convention** and **Z**.
- AI outputs include **explicit references** to symbols and numeric fields from input JSON.
- Lineage: each key metric can point to **inputs/events** at least at a defined granularity.

---

## 13) Locked decisions for v1.0 (summary)

| Topic | v1 choice |
|-------|-----------|
| Cost basis | **Weighted average cost** |
| VaR | **Parametric normal, 95%, 1-day** |
| FIFO | **Future pluggable extension** (not v1) |
| Historical VaR | **Future second method** (not v1) |

---

## 14) Product philosophy (guiding principles)

1. **Correctness > complexity** — deterministic calculations are critical.
2. **Events are the source of truth** — state reconstructable from the event log.
3. **Real-time thinking** — react to price updates and trades.
4. **AI is an interpreter, not an authority** — explains and suggests; core math stays deterministic.
5. **Design for explainability** — every output should be explainable (why PnL, why risk).

---
