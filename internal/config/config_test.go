package config

import (
	"testing"
	"time"
)

func TestLoadAgentBriefingDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test?sslmode=disable")
	t.Setenv("AGENT_BRIEFING_ENABLED", "")
	t.Setenv("AGENT_BRIEFING_SCHEDULER_ENABLED", "")
	t.Setenv("AGENT_BRIEFING_CRON", "")
	t.Setenv("AGENT_BRIEFING_TZ", "")
	t.Setenv("AGENT_MODEL", "")
	t.Setenv("AGENT_MAX_TOKENS", "")
	t.Setenv("AGENT_TEMPERATURE", "")
	t.Setenv("AGENT_MAX_TOOL_CALLS", "")
	t.Setenv("AGENT_MAX_TURNS", "")
	t.Setenv("AGENT_SESSION_TIMEOUT_SECONDS", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AgentBriefingEnabled {
		t.Fatalf("AgentBriefingEnabled = true, want false")
	}
	if cfg.AgentBriefingSchedulerEnabled {
		t.Fatalf("AgentBriefingSchedulerEnabled = true, want false")
	}
	if cfg.AgentBriefingCron != defaultAgentBriefingCron {
		t.Fatalf("AgentBriefingCron = %q, want %q", cfg.AgentBriefingCron, defaultAgentBriefingCron)
	}
	if cfg.AgentBriefingTZ != defaultAgentBriefingTZ {
		t.Fatalf("AgentBriefingTZ = %q, want %q", cfg.AgentBriefingTZ, defaultAgentBriefingTZ)
	}
	if cfg.AgentModel != defaultAgentModel {
		t.Fatalf("AgentModel = %q, want %q", cfg.AgentModel, defaultAgentModel)
	}
	if cfg.AgentMaxTokens != defaultAgentMaxTokens {
		t.Fatalf("AgentMaxTokens = %d, want %d", cfg.AgentMaxTokens, defaultAgentMaxTokens)
	}
	if cfg.AgentTemperature != defaultAgentTemperature {
		t.Fatalf("AgentTemperature = %v, want %v", cfg.AgentTemperature, defaultAgentTemperature)
	}
	if cfg.AgentMaxToolCalls != defaultAgentMaxToolCalls {
		t.Fatalf("AgentMaxToolCalls = %d, want %d", cfg.AgentMaxToolCalls, defaultAgentMaxToolCalls)
	}
	if cfg.AgentMaxTurns != defaultAgentMaxTurns {
		t.Fatalf("AgentMaxTurns = %d, want %d", cfg.AgentMaxTurns, defaultAgentMaxTurns)
	}
	if cfg.AgentSessionTimeout != time.Duration(defaultAgentSessionTimeoutSec)*time.Second {
		t.Fatalf(
			"AgentSessionTimeout = %v, want %v",
			cfg.AgentSessionTimeout,
			time.Duration(defaultAgentSessionTimeoutSec)*time.Second,
		)
	}
}

func TestLoadAgentBriefingOverridesAndClamps(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test?sslmode=disable")
	t.Setenv("AGENT_BRIEFING_ENABLED", "true")
	t.Setenv("AGENT_BRIEFING_SCHEDULER_ENABLED", "true")
	t.Setenv("AGENT_BRIEFING_CRON", "0 9 * * 1-5")
	t.Setenv("AGENT_BRIEFING_TZ", "UTC")
	t.Setenv("AGENT_MODEL", "claude-opus-4-7")
	t.Setenv("AGENT_MAX_TOKENS", "128")
	t.Setenv("AGENT_TEMPERATURE", "9")
	t.Setenv("AGENT_MAX_TOOL_CALLS", "0")
	t.Setenv("AGENT_MAX_TURNS", "0")
	t.Setenv("AGENT_SESSION_TIMEOUT_SECONDS", "3")
	t.Setenv("ANTHROPIC_API_KEY", "  key  ")
	t.Setenv("ANTHROPIC_BASE_URL", "  https://api.anthropic.com  ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.AgentBriefingEnabled {
		t.Fatalf("AgentBriefingEnabled = false, want true")
	}
	if !cfg.AgentBriefingSchedulerEnabled {
		t.Fatalf("AgentBriefingSchedulerEnabled = false, want true")
	}
	if cfg.AgentBriefingCron != "0 9 * * 1-5" {
		t.Fatalf("AgentBriefingCron = %q, want %q", cfg.AgentBriefingCron, "0 9 * * 1-5")
	}
	if cfg.AgentBriefingTZ != "UTC" {
		t.Fatalf("AgentBriefingTZ = %q, want %q", cfg.AgentBriefingTZ, "UTC")
	}
	if cfg.AgentModel != "claude-opus-4-7" {
		t.Fatalf("AgentModel = %q, want %q", cfg.AgentModel, "claude-opus-4-7")
	}
	if cfg.AgentMaxTokens != 256 {
		t.Fatalf("AgentMaxTokens = %d, want %d", cfg.AgentMaxTokens, 256)
	}
	if cfg.AgentTemperature != 1 {
		t.Fatalf("AgentTemperature = %v, want %v", cfg.AgentTemperature, 1.0)
	}
	if cfg.AgentMaxToolCalls != 1 {
		t.Fatalf("AgentMaxToolCalls = %d, want %d", cfg.AgentMaxToolCalls, 1)
	}
	if cfg.AgentMaxTurns != 1 {
		t.Fatalf("AgentMaxTurns = %d, want %d", cfg.AgentMaxTurns, 1)
	}
	if cfg.AgentSessionTimeout != 10*time.Second {
		t.Fatalf("AgentSessionTimeout = %v, want %v", cfg.AgentSessionTimeout, 10*time.Second)
	}
	if cfg.AnthropicAPIKey != "key" {
		t.Fatalf("AnthropicAPIKey = %q, want %q", cfg.AnthropicAPIKey, "key")
	}
	if cfg.AnthropicBaseURL != "https://api.anthropic.com" {
		t.Fatalf("AnthropicBaseURL = %q, want %q", cfg.AnthropicBaseURL, "https://api.anthropic.com")
	}
	if !cfg.AgentBriefingRuntimeEnabled() {
		t.Fatalf("AgentBriefingRuntimeEnabled() = false, want true")
	}
	if !cfg.AgentBriefingSchedulerRuntimeEnabled() {
		t.Fatalf("AgentBriefingSchedulerRuntimeEnabled() = false, want true")
	}
}

