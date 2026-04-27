# Phase 1 Agent Briefing Rollout

## Feature flags

- `AGENT_BRIEFING_ENABLED` (default `false`): enables briefing API routes and runtime wiring.
- `AGENT_BRIEFING_SCHEDULER_ENABLED` (default `false`): enables cron-based scheduled briefing runner.
- Scheduler runtime starts only when both flags are enabled and Anthropic credentials are configured.

## Environment variables and defaults

- `AGENT_BRIEFING_ENABLED=false`
- `AGENT_BRIEFING_SCHEDULER_ENABLED=false`
- `AGENT_BRIEFING_CRON="0 13 * * 1-5"`
- `AGENT_BRIEFING_TZ="America/New_York"`
- `ANTHROPIC_API_KEY` (required when briefing enabled)
- `ANTHROPIC_BASE_URL` (optional)
- `AGENT_MODEL="claude-sonnet-4.6"`
- `AGENT_MAX_TOKENS=2048`
- `AGENT_TEMPERATURE=0.2`
- `AGENT_MAX_TOOL_CALLS=12`
- `AGENT_MAX_TURNS=8`
- `AGENT_SESSION_TIMEOUT_SEC=120`

## API endpoints

- `POST /v1/portfolios/:id/briefings`
  - Body:
    - `user_input` (object, optional)
    - `model` (string, optional)
    - `temperature` (number, optional)
    - `max_tokens` (number, optional)
    - `scheduled` (bool, optional)
    - `run_date` (`YYYY-MM-DD`, optional)
  - Response:
    - `session_id`
    - `status`
    - `output` (present on immediate success)
- `GET /v1/portfolios/:id/briefings/latest`
- `GET /v1/portfolios/:id/briefings?limit=50&offset=0`
- `GET /v1/agent-sessions/:session_id/replay`

## Scheduler behavior

- Cron spec uses `AGENT_BRIEFING_CRON` and timezone `AGENT_BRIEFING_TZ`.
- On each tick:
  - list eligible portfolios (`owner_user_id IS NOT NULL`)
  - run `CreateBriefingScheduled` for each
  - idempotency suppresses duplicates per portfolio/day (`scheduled` trigger)
- Scheduler shutdown is graceful and tied to server cancellation/waitgroup flow.

## Observability

- Structured logs for lifecycle/tool events include:
  - `session_id`, `portfolio_id`, `trigger_source`, `tool_name`, `latency_ms`, `status`
- Prometheus metrics:
  - `agent_session_outcomes_total{status,trigger_source}`
  - `agent_tool_calls_total{tool_name,status}`
  - `agent_tool_latency_seconds{tool_name}`
  - `agent_validation_failures_total`
  - `agent_token_usage_total{direction}`

## Failure modes and runbook notes

- Missing Anthropic API key while flag enabled:
  - briefing runtime remains disabled; warning logged.
- Provider/tool failures:
  - session marked failed with error code and message.
- Validation failures:
  - session marked `invalid_output`; validation errors persisted.
- Scheduler bad cron/tz:
  - scheduler init logs warning and does not start.
- Duplicate scheduled run:
  - existing same-day session reused (no duplicate session insert).

## Launch checklist

1. Apply migrations in target environment.
2. Keep both feature flags disabled and deploy.
3. Smoke test with `AGENT_BRIEFING_ENABLED=true`, scheduler still disabled:
   - create on-demand briefing
   - fetch latest/list/replay
   - verify redaction in persisted prompts/tool payloads.
4. Enable scheduler flag and confirm:
   - cron start log appears
   - one scheduled session per portfolio/day
   - no duplicate scheduled rows.
5. Verify monitoring dashboards/alerts for agent metrics.

## Rollback plan

- Immediate rollback: set `AGENT_BRIEFING_ENABLED=false` (and scheduler false).
- API endpoints become unavailable for briefing routes while core portfolio/risk routes remain unaffected.
- Keep migration rollback only for emergency schema reversion; prefer feature-flag disable first.
