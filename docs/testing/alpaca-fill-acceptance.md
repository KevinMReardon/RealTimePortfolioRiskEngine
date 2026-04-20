# Alpaca fills — manual acceptance (slice 9)

Use a **paper** Alpaca account linked to the app’s Alpaca integration for the portfolio under test.

## Preconditions

- Backend env includes valid Alpaca API credentials and sync settings used by your deployment (`ALPACA_*` as documented in `.env.example`).
- Single-book deployment: default **`SINGLE_USER_APP=true`** keeps one portfolio row per account; Alpaca sync is selected from DB mapping (`portfolios.alpaca_account_mode`, `portfolios.alpaca_sync_enabled`) and stores the linked brokerage account id on **`portfolios.alpaca_account_id`** after successful **`GET /v2/account`**.
- You know the **sync interval** configured for Alpaca account / fill import (poll or job cadence).

## Steps

1. Note the portfolio’s internal positions (or cash / exposure) **before** the trade.
2. Place a **small** paper trade from Alpaca’s UI or API (not the app’s “Record trade” flow).
3. Wait **at most one sync interval** after the fill appears at the broker.
4. Refresh the app’s portfolio / positions view.

## Expectations

- **Internal portfolio updates** from the synced fill **without** using **Record trade** (or equivalent manual ingestion in the UI).
- After the fill is settled and sync has run, **`GET /v1/portfolios/{portfolio_id}/alpaca/reconciliation`** reports **no meaningful drift** (same as “no mismatch” / in sync with broker for positions and quantities you care about).
- Optional: **`GET .../alpaca/status`** shows healthy sync state if your deployment surfaces it.

## Automated coverage

- Unit tests in `internal/connectors/alpaca/fill_ingest_test.go` cover fill → `TradePayload` mapping and duplicate ingest (`OutcomeDuplicate`).
- Integration test `TestIntegration_AlpacaFillDuplicateDoesNotDoublePosition` (requires `INTEGRATION_DATABASE_URL` or `TEST_DATABASE_URL`) verifies one event row and stable `positions_projection` when the same Alpaca activity id is ingested twice.
