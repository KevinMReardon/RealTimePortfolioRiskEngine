# Engineering conventions (v1)

Cross-cutting rules for the Real-Time Portfolio & Risk Engine. Aligned with [PRD](../design/PRD.md) and [LLD](../design/LLD.md).

| Topic | Rule |
|-------|------|
| **Time** | Store timestamps in the database as `TIMESTAMPTZ` in **UTC**. Expose timestamps in APIs as **ISO-8601** with explicit offset (prefer `Z` for UTC), e.g. `2026-03-19T14:05:30.123Z`. |
| **Money** | In SQL use **`NUMERIC(20,8)`** (or equivalent fixed decimal). In Go use **`github.com/shopspring/decimal`** (or parse/emit decimal strings) for **persisted** monetary values and **aggregations**. Avoid `float64` for money that is stored or summed—it avoids rounding drift in financial paths. |
| **IDs** | Use **UUIDs** for `event_id`, `request_id`, and other primary correlation identifiers. **Validate** UUID format on ingest before accepting work. |
| **Config** | v1 loads configuration from **environment variables** only. In local development, use **direnv** and `.envrc` (gitignored) or your shell; in production, inject env from the orchestrator/secrets manager. Defer config libraries (e.g. Viper) until complexity warrants it. |

## Related environment variables (reference)

Examples (see `.envrc.example` when present):

- `DATABASE_URL` — Postgres connection string
- `ORDERING_WATERMARK_MS` — ordering buffer width
- `OPENAI_API_KEY` — AI layer (when implemented)

Add new variables to this table as the codebase grows.
