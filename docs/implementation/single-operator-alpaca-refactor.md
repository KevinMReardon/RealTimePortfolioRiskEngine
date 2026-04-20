# Single-Operator Mode Refactor

## Scope (Phase 1)

This app is intentionally scoped as a single-operator portfolio system:

- one operator account signs in
- one or more internal portfolios may exist, but UX is optimized for one primary book
- Alpaca linkage is portfolio-scoped in the database, not environment-scoped
- market prices remain global/provider-agnostic from the shared watchlist

## Portfolio-to-Alpaca Mapping

Portfolio linkage is now driven by catalog columns:

- `portfolios.alpaca_account_mode` (`paper` or `live`)
- `portfolios.alpaca_sync_enabled` (boolean toggle per portfolio)
- `portfolios.alpaca_account_id` (resolved from `GET /v2/account`)
- `alpaca_sync_state` holds per-portfolio cursor (`activities_after_time` / `activities_page_token`)

Environment now provides credentials per mode:

- `ALPACA_PAPER_KEY_ID` / `ALPACA_PAPER_SECRET_KEY` / `ALPACA_PAPER_BASE_URL`
- `ALPACA_LIVE_KEY_ID` / `ALPACA_LIVE_SECRET_KEY` / `ALPACA_LIVE_BASE_URL`

## Sync Worker Design

`cmd/server` starts a coordinator that:

1. lists `alpaca_sync_enabled=true` portfolios
2. selects the correct Alpaca REST client by `alpaca_account_mode`
3. runs the shared ingest/reconcile path per portfolio
4. persists independent cursors in `alpaca_sync_state`

## Price Feed Design

Price feed remains global and provider-agnostic:

- symbols come from watchlist
- no coupling to a specific user or Alpaca account
- provider can be `twelvedata` or `alpaca` market data independently of trade sync mode

## Phase 2 (Multi-User)

Planned extension path:

1. tighten ownership constraints and authorization checks
2. move secrets from process env into per-user/per-account secure storage
3. reuse this same portfolio/account sync abstraction with user-aware policy
