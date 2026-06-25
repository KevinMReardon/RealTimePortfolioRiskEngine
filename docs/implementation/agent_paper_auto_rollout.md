# Phase 3 — Bounded autonomous paper execution

## Overview

When enabled, proposals that pass deterministic policy and the self-critic are auto-approved and submitted to **Alpaca paper** via the same broker pipeline as manual HTTP submit (`internal/proposals/submit`).

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENT_EXEC_MODE` | `off` | `off` or `paper_auto` (exact strings) |
| `AGENT_CRITIC_MODEL` | (empty) | Optional critic model; defaults to `AGENT_MODEL` |
| `AGENT_PAPER_AUTO_TIMEOUT_SECONDS` | `300` | Max time for one post-briefing auto-submit batch |
| `AGENT_MAX_AUTO_SUBMITS_PER_SESSION` | `5` | Cap broker submits per briefing session |
| `POLICY_MODE` | `enforce` | Must be `enforce` for `paper_auto`; `monitor` forces effective mode `off` |

Also requires existing Phase 1–2 flags: `AGENT_BRIEFING_ENABLED=true`, `PROPOSALS_ENABLED=true`, portfolio linked to **paper** Alpaca keys.

## Daily equity anchor (policy daily loss)

Policy `max_daily_loss_pct` needs a row in `portfolio_equity_anchor` for each NY calendar day.

| Mechanism | When |
|-----------|------|
| Cron `30 9 * * 1-5` ET | Official open anchor (upserts) |
| Boot tick + retries (30s, 2m, 5m) | Same-day backfill after deploy |
| `EQUITY_ANCHOR_ENSURE_INTERVAL_MINUTES` (default 15) | Periodic ensure-if-missing for sync targets |
| Before briefing materialize | Ensure-if-missing for that portfolio (insert only; never overwrites) |

Env: `EQUITY_ANCHOR_ENSURE_INTERVAL_MINUTES` (default `15`). Portfolio must have `alpaca_sync_enabled` and keys for the periodic job; materialize ensure uses portfolio key material.

## Safety

- **Policy-as-code** is authoritative; the critic cannot override `EvaluateForBrokerSubmit`.
- **Paper only**: auto-submit is skipped when portfolio Alpaca `account_mode` is `live` or base URL is live.
- **Monitor mode**: `AGENT_EXEC_MODE=paper_auto` with `POLICY_MODE=monitor` is suppressed at config load (fail-closed).
- **Rollback**: set `AGENT_EXEC_MODE=off` and restart the server.

## Audit fields (`proposed_trades`)

- `critic_verdict` (JSONB): `{ "allow", "reason_code", "notes" }`
- `approval_source`: `human` | `paper_auto`
- Auto-approve leaves `approved_by_user_id` NULL.

## Evaluation (offline, no network)

```bash
go test ./internal/evaluation/... -count=1
```

Fixtures live under `internal/evaluation/testdata/`. Metrics include cumulative PnL, max drawdown, Sharpe (252-day annualization), and policy violation counts.

## Optional tool

`submit_proposal` (portfolio_id + proposal_id) calls the shared submit helper for **approved** proposals only; it does not expose broker credentials to the model.

## Next step (roadmap)

Phase 3 is the **code** slice. **Phase 4** in [agentic-trading-roadmap_1b28bd8b.plan.md](agentic-trading-roadmap_1b28bd8b.plan.md) is deploy always-on (not laptop-dependent), run a multi-week paper evaluation, stabilize bugs/UI, and confirm all existing functionality before Phase 5 (live) or other enhancements.
