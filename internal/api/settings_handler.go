package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

// SettingsStore reads and writes generic key/value settings from app_settings.
type SettingsStore interface {
	GetAppSetting(ctx context.Context, key string) (json.RawMessage, bool, error)
	UpsertAppSetting(ctx context.Context, key string, value json.RawMessage) error
	ListAppSettings(ctx context.Context) (map[string]json.RawMessage, error)
}

// settingDef is a catalog entry describing a single configurable setting.
type settingDef struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	Description    string `json:"description"`
	Group          string `json:"group"`
	Type           string `json:"type"` // "bool", "int", "string", "select"
	Default        any    `json:"default"`
	Options        []string `json:"options,omitempty"` // for "select" type
	RequiresRestart bool   `json:"requires_restart"`
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
		RequiresRestart: true,
	},
	{
		Key:             "agent_exec_mode",
		Label:           "Execution mode",
		Description:     "off = briefings only, no auto-trading. paper_auto = auto-submit to Alpaca paper accounts after critic approval.",
		Group:           "Agent",
		Type:            "select",
		Default:         "off",
		Options:         []string{"off", "paper_auto"},
		RequiresRestart: true,
	},
	{
		Key:             "agent_briefing_cron",
		Label:           "Briefing cron schedule",
		Description:     "Standard 5-field cron expression for scheduled briefings (e.g. '0 9-16 * * 1-5' = hourly 9am–4pm weekdays ET).",
		Group:           "Agent",
		Type:            "string",
		Default:         "0 9-16 * * 1-5",
		RequiresRestart: true,
	},
	{
		Key:             "agent_model",
		Label:           "AI model",
		Description:     "Anthropic model used for briefings (e.g. claude-sonnet-4-5, claude-opus-4-5).",
		Group:           "Agent",
		Type:            "string",
		Default:         "claude-sonnet-4-5",
		RequiresRestart: true,
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
		RequiresRestart: true,
	},
	// --- Policy ---
	{
		Key:             "trading_halt",
		Label:           "Trading kill switch",
		Description:     "When on, blocks all order submission immediately. Use to pause trading without restarting the server.",
		Group:           "Policy",
		Type:            "bool",
		Default:         false,
		RequiresRestart: true,
	},
	{
		Key:             "policy_mode",
		Label:           "Policy enforcement mode",
		Description:     "enforce = policy violations block trades. monitor = violations are logged only (paper_auto will be disabled while in monitor mode).",
		Group:           "Policy",
		Type:            "select",
		Default:         "enforce",
		Options:         []string{"enforce", "monitor"},
		RequiresRestart: true,
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
		Default:         60,
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

func getSettingsHandler(store SettingsStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		stored, err := store.ListAppSettings(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load settings"})
			return
		}

		out := make([]settingResponse, 0, len(settingCatalog))
		for _, def := range settingCatalog {
			sr := settingResponse{settingDef: def, Value: def.Default}
			if raw, ok := stored[def.Key]; ok {
				var v any
				if err := json.Unmarshal(raw, &v); err == nil {
					sr.Value = v
				}
			}
			out = append(out, sr)
		}
		c.JSON(http.StatusOK, settingsListResponse{Settings: out})
	}
}

func patchSettingsHandler(store SettingsStore) gin.HandlerFunc {
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
		c.JSON(http.StatusOK, settingsPatchResponse{Updated: updated})
	}
}
