package config

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
)

// App settings keys persisted in app_settings (must match api setting catalog).
const (
	SettingAgentBriefingEnabled          = "agent_briefing_enabled"
	SettingAgentBriefingSchedulerEnabled = "agent_briefing_scheduler_enabled"
	SettingAgentExecMode                 = "agent_exec_mode"
	SettingAgentBriefingCron             = "agent_briefing_cron"
	SettingAgentBriefingTZ               = "agent_briefing_tz"
	SettingAgentBriefingCooldownMinutes  = "agent_briefing_cooldown_minutes"
	SettingAgentModel                    = "agent_model"
	SettingAgentMaxTurns                 = "agent_max_turns"
	SettingAgentMaxToolCalls             = "agent_max_tool_calls"
	SettingAgentSessionTimeoutSeconds    = "agent_session_timeout_seconds"
	SettingAgentMaxTokens                = "agent_max_tokens"
	SettingAgentTemperature              = "agent_temperature"
	SettingTradingHalt                   = "trading_halt"
	SettingPolicyMode                    = "policy_mode"
	SettingProposalsEnabled              = "proposals_enabled"
	SettingPolicyMaxOrderNotionalUSD     = "policy_max_order_notional_usd"
	SettingPolicyMaxDailyNotionalUSD     = "policy_max_daily_notional_usd"
	SettingPolicyMaxPositionPct          = "policy_max_position_pct"
	SettingPolicyMaxDailyLossPct         = "policy_max_daily_loss_pct"
	SettingPolicyMaxOrdersPerMinute      = "policy_max_orders_per_minute"
	SettingPriceFeedEnabled              = "price_feed_enabled"
	SettingPriceFeedProvider             = "price_feed_provider"
	SettingPriceFeedPollSeconds          = "price_feed_poll_seconds"
	SettingPriceFeedMaxQuoteAgeMS        = "price_feed_max_quote_age_ms"
	SettingPriceFeedDedupWindowMS        = "price_feed_dedup_window_ms"
)

// AppSettingsReader loads persisted settings rows.
type AppSettingsReader interface {
	ListAppSettings(ctx context.Context) (map[string]json.RawMessage, error)
}

// OverlayAppSettings applies DB overrides on top of env-loaded config.
// Env provides secrets and infrastructure; any key present in stored wins.
func OverlayAppSettings(base Config, stored map[string]json.RawMessage) (Config, error) {
	if len(stored) == 0 {
		return reconcileExecMode(base), nil
	}
	out := base
	var err error

	if raw, ok := stored[SettingAgentBriefingEnabled]; ok {
		if out.AgentBriefingEnabled, err = parseBool(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingAgentBriefingEnabled, err)
		}
	}
	if raw, ok := stored[SettingAgentBriefingSchedulerEnabled]; ok {
		if out.AgentBriefingSchedulerEnabled, err = parseBool(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingAgentBriefingSchedulerEnabled, err)
		}
	}
	if raw, ok := stored[SettingAgentExecMode]; ok {
		mode, err := parseString(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingAgentExecMode, err)
		}
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case AgentExecModeOff, AgentExecModePaperAuto:
			out.AgentExecMode = mode
		default:
			return Config{}, fmt.Errorf("%s: invalid %q", SettingAgentExecMode, mode)
		}
	}
	if raw, ok := stored[SettingAgentBriefingCron]; ok {
		if out.AgentBriefingCron, err = parseString(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingAgentBriefingCron, err)
		}
	}
	if raw, ok := stored[SettingAgentBriefingTZ]; ok {
		if out.AgentBriefingTZ, err = parseString(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingAgentBriefingTZ, err)
		}
	}
	if raw, ok := stored[SettingAgentBriefingCooldownMinutes]; ok {
		mins, err := parseInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingAgentBriefingCooldownMinutes, err)
		}
		if mins < 0 {
			mins = 0
		}
		out.AgentBriefingCooldown = time.Duration(mins) * time.Minute
	}
	if raw, ok := stored[SettingAgentModel]; ok {
		if out.AgentModel, err = parseString(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingAgentModel, err)
		}
	}
	if raw, ok := stored[SettingAgentMaxTurns]; ok {
		if out.AgentMaxTurns, err = parseInt(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingAgentMaxTurns, err)
		}
		if out.AgentMaxTurns < 1 {
			out.AgentMaxTurns = 1
		}
	}
	if raw, ok := stored[SettingAgentMaxToolCalls]; ok {
		if out.AgentMaxToolCalls, err = parseInt(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingAgentMaxToolCalls, err)
		}
		if out.AgentMaxToolCalls < 1 {
			out.AgentMaxToolCalls = 1
		}
	}
	if raw, ok := stored[SettingAgentSessionTimeoutSeconds]; ok {
		sec, err := parseInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingAgentSessionTimeoutSeconds, err)
		}
		if sec < 10 {
			sec = 10
		}
		out.AgentSessionTimeout = time.Duration(sec) * time.Second
	}
	if raw, ok := stored[SettingAgentMaxTokens]; ok {
		n, err := parseInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingAgentMaxTokens, err)
		}
		if n < 256 {
			n = 256
		}
		out.AgentMaxTokens = n
	}
	if raw, ok := stored[SettingAgentTemperature]; ok {
		f, err := parseFloat(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingAgentTemperature, err)
		}
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		out.AgentTemperature = f
	}
	if raw, ok := stored[SettingTradingHalt]; ok {
		if out.TradingHalt, err = parseBool(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingTradingHalt, err)
		}
	}
	if raw, ok := stored[SettingPolicyMode]; ok {
		mode, err := parseString(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingPolicyMode, err)
		}
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "monitor":
			out.PolicyMode = policy.ModeMonitor
		case "enforce":
			out.PolicyMode = policy.ModeEnforce
		default:
			return Config{}, fmt.Errorf("%s: invalid %q", SettingPolicyMode, mode)
		}
	}
	if raw, ok := stored[SettingProposalsEnabled]; ok {
		if out.ProposalsEnabled, err = parseBool(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingProposalsEnabled, err)
		}
	}
	if raw, ok := stored[SettingPolicyMaxOrderNotionalUSD]; ok {
		if out.PolicyMaxOrderNotionalUSD, err = parseDecimal(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingPolicyMaxOrderNotionalUSD, err)
		}
	}
	if raw, ok := stored[SettingPolicyMaxDailyNotionalUSD]; ok {
		if out.PolicyMaxDailyNotionalUSD, err = parseDecimal(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingPolicyMaxDailyNotionalUSD, err)
		}
	}
	if raw, ok := stored[SettingPolicyMaxPositionPct]; ok {
		if out.PolicyMaxPositionPct, err = parseDecimal(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingPolicyMaxPositionPct, err)
		}
	}
	if raw, ok := stored[SettingPolicyMaxDailyLossPct]; ok {
		if out.PolicyMaxDailyLossPct, err = parseDecimal(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingPolicyMaxDailyLossPct, err)
		}
	}
	if raw, ok := stored[SettingPolicyMaxOrdersPerMinute]; ok {
		if out.PolicyMaxOrdersPerMinute, err = parseInt(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingPolicyMaxOrdersPerMinute, err)
		}
	}
	if raw, ok := stored[SettingPriceFeedEnabled]; ok {
		if out.PriceFeedEnabled, err = parseBool(raw); err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingPriceFeedEnabled, err)
		}
	}
	if raw, ok := stored[SettingPriceFeedProvider]; ok {
		prov, err := parseString(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingPriceFeedProvider, err)
		}
		switch strings.ToLower(strings.TrimSpace(prov)) {
		case "twelvedata", "alpaca":
			out.PriceFeedProvider = prov
		default:
			return Config{}, fmt.Errorf("%s: invalid %q", SettingPriceFeedProvider, prov)
		}
	}
	if raw, ok := stored[SettingPriceFeedPollSeconds]; ok {
		sec, err := parseInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingPriceFeedPollSeconds, err)
		}
		if sec < 1 {
			sec = 1
		}
		out.PriceFeedPollInterval = time.Duration(sec) * time.Second
	}
	if raw, ok := stored[SettingPriceFeedMaxQuoteAgeMS]; ok {
		ms, err := parseInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingPriceFeedMaxQuoteAgeMS, err)
		}
		out.PriceFeedMaxQuoteAge = time.Duration(ms) * time.Millisecond
	}
	if raw, ok := stored[SettingPriceFeedDedupWindowMS]; ok {
		ms, err := parseInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", SettingPriceFeedDedupWindowMS, err)
		}
		out.PriceFeedDedupWindow = time.Duration(ms) * time.Millisecond
	}

	return reconcileExecMode(out), nil
}

// LoadWithAppSettings loads env config and overlays persisted app_settings when reader is non-nil.
func LoadWithAppSettings(ctx context.Context, reader AppSettingsReader) (Config, error) {
	base, err := Load()
	if err != nil {
		return Config{}, err
	}
	if reader == nil {
		return base, nil
	}
	stored, err := reader.ListAppSettings(ctx)
	if err != nil {
		return Config{}, err
	}
	return OverlayAppSettings(base, stored)
}

func reconcileExecMode(cfg Config) Config {
	execRequested := strings.ToLower(strings.TrimSpace(cfg.AgentExecMode))
	if execRequested == "" {
		execRequested = AgentExecModeOff
	}
	switch execRequested {
	case AgentExecModeOff, AgentExecModePaperAuto:
	default:
		execRequested = AgentExecModeOff
	}
	cfg.AgentExecMode = execRequested
	cfg.AgentExecPaperAutoSuppressedDueToMonitorPolicy = false
	if execRequested == AgentExecModePaperAuto && cfg.PolicyMode == policy.ModeMonitor {
		cfg.AgentExecMode = AgentExecModeOff
		cfg.AgentExecPaperAutoSuppressedDueToMonitorPolicy = true
	}
	return cfg
}

func parseBool(raw json.RawMessage) (bool, error) {
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, err
	}
	return v, nil
}

func parseInt(raw json.RawMessage) (int, error) {
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		// JSON numbers often decode as float64 via interface{} round-trip.
		var f float64
		if err2 := json.Unmarshal(raw, &f); err2 != nil {
			return 0, err
		}
		return int(f), nil
	}
	return v, nil
}

func parseString(raw json.RawMessage) (string, error) {
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	return strings.TrimSpace(v), nil
}

func parseFloat(raw json.RawMessage) (float64, error) {
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, err
	}
	return v, nil
}

func parseDecimal(raw json.RawMessage) (decimal.Decimal, error) {
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		// Try string representation.
		var s string
		if err2 := json.Unmarshal(raw, &s); err2 != nil {
			return decimal.Zero, err
		}
		d, err2 := decimal.NewFromString(strings.TrimSpace(s))
		if err2 != nil {
			return decimal.Zero, err2
		}
		return d, nil
	}
	return decimal.NewFromFloat(f), nil
}

// CatalogValues maps persisted setting keys to their effective values from cfg.
// The price_feed_watchlist key is intentionally omitted here — it is managed by the
// pricefeed runner and hydrator; the settings page reads it directly from app_settings.
func CatalogValues(cfg Config) map[string]any {
	return map[string]any{
		SettingAgentBriefingEnabled:          cfg.AgentBriefingEnabled,
		SettingAgentBriefingSchedulerEnabled: cfg.AgentBriefingSchedulerEnabled,
		SettingAgentExecMode:                 cfg.AgentExecMode,
		SettingAgentBriefingCron:             cfg.AgentBriefingCron,
		SettingAgentBriefingTZ:               cfg.AgentBriefingTZ,
		SettingAgentBriefingCooldownMinutes:  int(cfg.AgentBriefingCooldown / time.Minute),
		SettingAgentModel:                    cfg.AgentModel,
		SettingAgentMaxTurns:                 cfg.AgentMaxTurns,
		SettingAgentMaxToolCalls:             cfg.AgentMaxToolCalls,
		SettingAgentSessionTimeoutSeconds:    int(cfg.AgentSessionTimeout / time.Second),
		SettingAgentMaxTokens:                cfg.AgentMaxTokens,
		SettingAgentTemperature:              cfg.AgentTemperature,
		SettingTradingHalt:                   cfg.TradingHalt,
		SettingPolicyMode:                    string(cfg.PolicyMode),
		SettingProposalsEnabled:              cfg.ProposalsEnabled,
		SettingPolicyMaxOrderNotionalUSD:     cfg.PolicyMaxOrderNotionalUSD.String(),
		SettingPolicyMaxDailyNotionalUSD:     cfg.PolicyMaxDailyNotionalUSD.String(),
		SettingPolicyMaxPositionPct:          cfg.PolicyMaxPositionPct.String(),
		SettingPolicyMaxDailyLossPct:         cfg.PolicyMaxDailyLossPct.String(),
		SettingPolicyMaxOrdersPerMinute:      cfg.PolicyMaxOrdersPerMinute,
		SettingPriceFeedEnabled:              cfg.PriceFeedEnabled,
		SettingPriceFeedProvider:             cfg.PriceFeedProvider,
		SettingPriceFeedPollSeconds:          int(cfg.PriceFeedPollInterval / time.Second),
		SettingPriceFeedMaxQuoteAgeMS:        int(cfg.PriceFeedMaxQuoteAge / time.Millisecond),
		SettingPriceFeedDedupWindowMS:        int(cfg.PriceFeedDedupWindow / time.Millisecond),
	}
}
