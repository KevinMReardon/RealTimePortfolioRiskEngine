#!/usr/bin/env bash
# Smoke-test HTTP API against a running server (default http://127.0.0.1:8080).
#
# Prerequisites:
#   - Postgres migrated:  make migrate-up   (DATABASE_URL set)
#   - Server running:     make run         (same DATABASE_URL)
#
# Optional env:
#   BASE=http://127.0.0.1:8080
#   PORTFOLIO_ID=<uuid>   # default: random v4 (must NOT be a price-stream shard UUID)
#
set -euo pipefail

BASE="${BASE:-http://127.0.0.1:8080}"
PORTFOLIO_ID="${PORTFOLIO_ID:-}"
if [[ -z "$PORTFOLIO_ID" ]]; then
	if command -v uuidgen >/dev/null 2>&1; then
		PORTFOLIO_ID="$(uuidgen | tr '[:upper:]' '[:lower:]')"
	else
		echo "Set PORTFOLIO_ID or install uuidgen" >&2
		exit 1
	fi
fi

hdr=(-H "Content-Type: application/json" -H "X-Request-ID: $(uuidgen 2>/dev/null || echo 00000000-0000-4000-8000-000000000001)")

echo "=== GET /health ==="
curl -sS -w "\nHTTP %{http_code}\n" "$BASE/health"
echo

echo "=== POST /v1/trades (creates portfolio stream) ==="
curl -sS -w "\nHTTP %{http_code}\n" "${hdr[@]}" -X POST "$BASE/v1/trades" \
	-d "{\"portfolio_id\":\"$PORTFOLIO_ID\",\"idempotency_key\":\"smoke-trade-1\",\"source\":\"smoke_api\",\"trade\":{\"trade_id\":\"t1\",\"symbol\":\"AAPL\",\"side\":\"BUY\",\"quantity\":\"10\",\"price\":\"100\",\"currency\":\"USD\"}}"
echo

echo "Waiting for trade worker to apply (3s)..."
sleep 3

echo "=== POST /v1/prices (AAPL mark) ==="
curl -sS -w "\nHTTP %{http_code}\n" "${hdr[@]}" -X POST "$BASE/v1/prices" \
	-d '{"idempotency_key":"smoke-px-1","source":"smoke_api","price":{"symbol":"AAPL","price":"150","currency":"USD","source_sequence":1}}'
echo

echo "Waiting for price worker to apply (3s)..."
sleep 3

echo "=== GET /v1/portfolios/$PORTFOLIO_ID ==="
curl -sS -w "\nHTTP %{http_code}\n" "$BASE/v1/portfolios/$PORTFOLIO_ID"
echo

echo "=== GET /v1/portfolios/$PORTFOLIO_ID/risk ==="
curl -sS -w "\nHTTP %{http_code}\n" "$BASE/v1/portfolios/$PORTFOLIO_ID/risk"
echo

echo "=== POST /v1/portfolios/$PORTFOLIO_ID/scenarios ==="
curl -sS -w "\nHTTP %{http_code}\n" "${hdr[@]}" -X POST "$BASE/v1/portfolios/$PORTFOLIO_ID/scenarios" \
	-d '{"shocks":[{"symbol":"AAPL","type":"PCT","value":"-0.1"}]}'
echo

echo "=== POST /v1/portfolios/$PORTFOLIO_ID/insights/explain ==="
curl -sS -w "\nHTTP %{http_code}\n" "${hdr[@]}" -X POST "$BASE/v1/portfolios/$PORTFOLIO_ID/insights/explain" \
	-d '{}'
echo

echo "Done. PORTFOLIO_ID=$PORTFOLIO_ID"
