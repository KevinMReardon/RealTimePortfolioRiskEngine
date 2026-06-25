package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/runtime"
)

// SettingsStore reads and writes generic key/value settings from app_settings.
type SettingsStore interface {
	GetAppSetting(ctx context.Context, key string) (json.RawMessage, bool, error)
	UpsertAppSetting(ctx context.Context, key string, value json.RawMessage) error
	ListAppSettings(ctx context.Context) (map[string]json.RawMessage, error)
}

// settingDef is a catalog entry describing a single configurable setting.
type settingDef struct {
	Key             string   `json:"key"`
	Label           string   `json:"label"`
	Description     string   `json:"description"`
	Group           string   `json:"group"`
	Type            string   `json:"type"` // "bool", "int", "string", "select"
	Default         any      `json:"default"`
	Options         []string `json:"options,omitempty"` // for "select" type
	RequiresRestart bool     `json:"requires_restart"`
}

// settingCatalog is the authoritative list of tunable settings exposed in the UI.
// Secrets (API keys, DB credentials) are intentionally excluded.
var settingCatalog = []settingDef{
	// --- Agent ---
	{
		Key:             "agent_briefing_enabled",
		Label:           "Enable AI briefings",
		Description:     "Turns the Claude briefing service on or off. When off, no briefings run regardless of scheduler.",
		Group:           "Agent",
		Type:            "bool",
		Default:         false,
		RequiresRestart: true,
	},
	{
		Key:             "agent_briefing_scheduler_enabled",
		Label:           "Enable scheduled briefings",
		Description:     "Automatically runs briefings on the configured cron schedule. Requires briefings to be enabled.",
		Group:           "Agent",
		Type:            "bool",
		Default:         false,
		RequiresRestart: false,
	},
	{
		Key:             "agent_exec_mode",
		Label:           "Execution mode",
		Description:     "off = briefings only, no auto-trading. paper_auto = auto-submit to Alpaca paper accounts after critic approval.",
		Group:           "Agent",
		Type:            "select",
		Default:         "off",
		Options:         []string{"off", "paper_auto"},
		RequiresRestart: false,
	},
	{
		Key:             "agent_briefing_cron",
		Label:           "Briefing cron schedule",
		Description:     "Standard 5-field cron expression for scheduled briefings (e.g. '0 9-16 * * 1-5' = hourly 9am–4pm weekdays ET). Hot-reloaded.",
		Group:           "Agent",
		Type:            "string",
		Default:         "0 9-16 * * 1-5",
		RequiresRestart: false,
	},
	{
		Key:             "agent_briefing_tz",
		Label:           "Briefing cron timezone",
		Description:     "IANA timezone for interpreting the briefing cron schedule (e.g. America/New_York). Hot-reloaded.",
		Group:           "Agent",
		Type:            "string",
		Default:         "America/New_York",
		RequiresRestart: false,
	},
	{
		Key:             "agent_briefing_cooldown_minutes",
		Label:           "Briefing cooldown (minutes)",
		Description:     "Minimum wait after a successful scheduled briefing before another scheduled briefing can run. 0 disables cooldown.",
		Group:           "Agent",
		Type:            "int",
		Default:         30,
		RequiresRestart: false,
	},
	{
		Key:             "agent_model",
		Label:           "AI model",
		Description:     "Anthropic model used for briefings (e.g. claude-sonnet-4.6, claude-opus-4-5).",
		Group:           "Agent",
		Type:            "string",
		Default:         "claude-sonnet-4.6",
		RequiresRestart: false,
	},
	{
		Key:             "agent_max_turns",
		Label:           "Max conversation turns",
		Description:     "Maximum back-and-forth turns the agent can take per briefing session before it is cut off.",
		Group:           "Agent",
		Type:            "int",
		Default:         8,
		RequiresRestart: true,
	},
	{
		Key:             "agent_max_tool_calls",
		Label:           "Max tool calls",
		Description:     "Maximum number of tool calls (data lookups) the agent can make within one briefing session.",
		Group:           "Agent",
		Type:            "int",
		Default:         12,
		RequiresRestart: true,
	},
	{
		Key:             "agent_session_timeout_seconds",
		Label:           "Session timeout (seconds)",
		Description:     "Hard time limit for one briefing session. Sessions exceeding this are marked failed.",
		Group:           "Agent",
		Type:            "int",
		Default:         120,
		RequiresRestart: false,
	},
	{
		Key:             "agent_max_tokens",
		Label:           "Max output tokens",
		Description:     "Maximum tokens the model may generate per briefing response. Increase for longer, more detailed briefings.",
		Group:           "Agent",
		Type:            "int",
		Default:         2048,
		RequiresRestart: false,
	},
	{
		Key:             "agent_temperature",
		Label:           "Model temperature",
		Description:     "Sampling temperature for briefing responses (0.0 = deterministic, 1.0 = very creative). Recommended: 0.1–0.3.",
		Group:           "Agent",
		Type:            "number",
		Default:         0.2,
		RequiresRestart: false,
	},
	// --- Policy ---
	{
		Key:             "trading_halt",
		Label:           "Trading kill switch",
		Description:     "When on, blocks all order submission immediately. Use to pause trading without restarting the server.",
		Group:           "Policy",
		Type:            "bool",
		Default:         false,
		RequiresRestart: false,
	},
	{
		Key:             "policy_mode",
		Label:           "Policy enforcement mode",
		Description:     "enforce = policy violations block trades. monitor = violations are logged only (paper_auto will be disabled while in monitor mode).",
		Group:           "Policy",
		Type:            "select",
		Default:         "enforce",
		Options:         []string{"enforce", "monitor"},
		RequiresRestart: false,
	},
	{
		Key:             "proposals_enabled",
		Label:           "Enable trade proposals",
		Description:     "When on, briefings materialise trade ideas into proposal rows that can be approved and submitted.",
		Group:           "Policy",
		Type:            "bool",
		Default:         false,
		RequiresRestart: true,
	},
	{
		Key:             "policy_max_order_notional_usd",
		Label:           "Max order size (USD)",
		Description:     "Hard cap on the notional value of any single order in USD. 0 = no limit.",
		Group:           "Policy",
		Type:            "number",
		Default:         0,
		RequiresRestart: false,
	},
	{
		Key:             "policy_max_daily_notional_usd",
		Label:           "Max daily trading volume (USD)",
		Description:     "Cap on total notional value executed per calendar day in USD. 0 = no limit.",
		Group:           "Policy",
		Type:            "number",
		Default:         0,
		RequiresRestart: false,
	},
	{
		Key:             "policy_max_position_pct",
		Label:           "Max position size (% of equity)",
		Description:     "Maximum post-trade position market value as a percentage of portfolio equity (e.g. 20 = 20%). 0 = no limit.",
		Group:           "Policy",
		Type:            "number",
		Default:         0,
		RequiresRestart: false,
	},
	{
		Key:             "policy_max_daily_loss_pct",
		Label:           "Max daily loss (% of equity)",
		Description:     "Stops trading when portfolio drawdown since open exceeds this percentage of equity (e.g. 2 = 2%). 0 = no limit.",
		Group:           "Policy",
		Type:            "number",
		Default:         0,
		RequiresRestart: false,
	},
	{
		Key:             "policy_max_orders_per_minute",
		Label:           "Max orders per minute",
		Description:     "Rate cap on order submissions per minute. 0 = no limit.",
		Group:           "Policy",
		Type:            "int",
		Default:         0,
		RequiresRestart: false,
	},
	// --- Price Feed ---
	{
		Key:             "price_feed_enabled",
		Label:           "Enable price feed",
		Description:     "Starts the automated provider polling loop that keeps symbol prices up to date.",
		Group:           "Price Feed",
		Type:            "bool",
		Default:         false,
		RequiresRestart: true,
	},
	{
		Key:             "price_feed_provider",
		Label:           "Price provider",
		Description:     "twelvedata uses the Twelve Data API key. alpaca uses the Alpaca Market Data REST API (no separate key needed).",
		Group:           "Price Feed",
		Type:            "select",
		Default:         "twelvedata",
		Options:         []string{"twelvedata", "alpaca"},
		RequiresRestart: true,
	},
	{
		Key:             "price_feed_poll_seconds",
		Label:           "Price poll interval (seconds)",
		Description:     "How often the price feed fetches fresh quotes from the provider. Lower values cost more API quota.",
		Group:           "Price Feed",
		Type:            "int",
		Default:         900,
		RequiresRestart: true,
	},
	{
		Key:             "price_feed_watchlist",
		Label:           "Watched symbols",
		Description:     "Comma-separated list of symbols actively tracked for price data (e.g. AAPL,MSFT,BTC-USD). The briefing agent also uses this list to discover new trade opportunities.",
		Group:           "Price Feed",
		Type:            "string",
		Default:         "",
		RequiresRestart: false,
	},
	{
		Key:             "price_feed_max_quote_age_ms",
		Label:           "Max quote age (ms)",
		Description:     "Reject upstream quotes older than this many milliseconds. 0 = no staleness check. Default 1800000 (30 min).",
		Group:           "Price Feed",
		Type:            "int",
		Default:         1800000,
		RequiresRestart: true,
	},
	{
		Key:             "price_feed_dedup_window_ms",
		Label:           "Dedup window (ms)",
		Description:     "Skip writing an unchanged price if the same value was written within this window. 0 = disable. Default 60000 (1 min).",
		Group:           "Price Feed",
		Type:            "int",
		Default:         60000,
		RequiresRestart: true,
	},
}

type settingResponse struct {
	settingDef
	Value any `json:"value"` // current value (from DB if set, default otherwise)
}

type settingsListResponse struct {
	Settings []settingResponse `json:"settings"`
}

type settingsPatchRequest struct {
	Settings map[string]json.RawMessage `json:"settings"`
}

type settingsPatchResponse struct {
	Updated []string `json:"updated"`
}

func getSettingsHandler(holder *config.ConfigHolder) gin.HandlerFunc {
	return func(c *gin.Context) {
		effective := config.CatalogValues(holder.Get())
		out := make([]settingResponse, 0, len(settingCatalog))
		for _, def := range settingCatalog {
			sr := settingResponse{settingDef: def, Value: def.Default}
			if v, ok := effective[def.Key]; ok {
				sr.Value = v
			}
			out = append(out, sr)
		}
		c.JSON(http.StatusOK, settingsListResponse{Settings: out})
	}
}

func patchSettingsHandler(store SettingsStore, reloader *runtime.SettingsReloader) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req settingsPatchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}

		// Build an allowed-keys set from the catalog.
		allowed := make(map[string]struct{}, len(settingCatalog))
		for _, def := range settingCatalog {
			allowed[def.Key] = struct{}{}
		}

		var updated []string
		for key, val := range req.Settings {
			if _, ok := allowed[key]; !ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": "unknown setting key: " + key})
				return
			}
			if err := store.UpsertAppSetting(c.Request.Context(), key, val); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save setting: " + key})
				return
			}
			updated = append(updated, key)
		}
		if reloader != nil {
			if _, err := reloader.Reload(c.Request.Context()); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "settings saved but failed to apply: " + err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, settingsPatchResponse{Updated: updated})
	}
}
