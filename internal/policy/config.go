package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/shopspring/decimal"
)

// Config holds policy limits (typically loaded from env / file by the application).
type Config struct {
	Mode Mode

	SymbolWhitelist []string // if non-empty, symbol must be in this set (uppercase normalized)
	SymbolBlacklist []string // deny if symbol in this set

	MaxOrderNotionalUSD    decimal.Decimal
	MaxDailyNotionalUSD    decimal.Decimal // zero disables daily notional cap
	MaxPositionPct         decimal.Decimal // max post-trade position MV as % of equity (e.g. 25 = 25%)
	MaxDailyLossPct        decimal.Decimal // vs EquityAnchor; zero disables (e.g. 2 = 2% drawdown from anchor triggers deny)
	MaxOrdersPerMinute     int           // zero disables rate limit

	// PolicyVersion is optional semver string embedded in hashes for audit only.
	PolicyVersion string
}

// DefaultConfig returns conservative zeros / empties suitable as a baseline (many rules disabled until set).
func DefaultConfig() Config {
	return Config{
		Mode: ModeEnforce,
	}
}

// policyConfigHashPayload is a stable JSON shape for SHA-256 (sorted slices).
type policyConfigHashPayload struct {
	Mode                   string   `json:"mode"`
	SymbolWhitelist        []string `json:"symbol_whitelist"`
	SymbolBlacklist        []string `json:"symbol_blacklist"`
	MaxOrderNotionalUSD    string   `json:"max_order_notional_usd"`
	MaxDailyNotionalUSD    string   `json:"max_daily_notional_usd"`
	MaxPositionPct         string   `json:"max_position_pct"`
	MaxDailyLossPct        string   `json:"max_daily_loss_pct"`
	MaxOrdersPerMinute     int      `json:"max_orders_per_minute"`
	PolicyVersion          string   `json:"policy_version"`
}

// PolicyConfigHash returns SHA-256 hex of canonical JSON for cfg (for proposed_trades.policy_config_hash).
func PolicyConfigHash(cfg Config) string {
	w := append([]string{}, cfg.SymbolWhitelist...)
	sort.Strings(w)
	b := append([]string{}, cfg.SymbolBlacklist...)
	sort.Strings(b)
	payload := policyConfigHashPayload{
		Mode:                string(cfg.Mode),
		SymbolWhitelist:     w,
		SymbolBlacklist:     b,
		MaxOrderNotionalUSD: cfg.MaxOrderNotionalUSD.StringFixed(12),
		MaxDailyNotionalUSD: cfg.MaxDailyNotionalUSD.StringFixed(12),
		MaxPositionPct:      cfg.MaxPositionPct.StringFixed(12),
		MaxDailyLossPct:     cfg.MaxDailyLossPct.StringFixed(12),
		MaxOrdersPerMinute:  cfg.MaxOrdersPerMinute,
		PolicyVersion:       cfg.PolicyVersion,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		// Marshaling a plain struct should not fail; fail closed with empty if it ever does.
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// CanonicalPolicyConfigBytes returns the JSON bytes used for PolicyConfigHash (for tests / replay tooling).
func CanonicalPolicyConfigBytes(cfg Config) []byte {
	w := append([]string{}, cfg.SymbolWhitelist...)
	sort.Strings(w)
	b := append([]string{}, cfg.SymbolBlacklist...)
	sort.Strings(b)
	payload := policyConfigHashPayload{
		Mode:                string(cfg.Mode),
		SymbolWhitelist:     w,
		SymbolBlacklist:     b,
		MaxOrderNotionalUSD: cfg.MaxOrderNotionalUSD.StringFixed(12),
		MaxDailyNotionalUSD: cfg.MaxDailyNotionalUSD.StringFixed(12),
		MaxPositionPct:      cfg.MaxPositionPct.StringFixed(12),
		MaxDailyLossPct:     cfg.MaxDailyLossPct.StringFixed(12),
		MaxOrdersPerMinute:  cfg.MaxOrdersPerMinute,
		PolicyVersion:       cfg.PolicyVersion,
	}
	raw, _ := json.Marshal(payload)
	return raw
}

// NormalizeSymbol uppercases and trims for whitelist/blacklist lookups.
func NormalizeSymbol(s string) string {
	// ASCII uppercase for US symbols; domain regex already restricts charset.
	b := bytes.TrimSpace([]byte(s))
	out := make([]byte, len(b))
	for i := range b {
		c := b[i]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		out[i] = c
	}
	return string(out)
}
