package config

import (
	"encoding/json"
	"testing"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
)

func TestOverlayAppSettings_DBOverridesEnv(t *testing.T) {
	base := Config{
		AgentExecMode:       AgentExecModeOff,
		TradingHalt:         false,
		PolicyMode:          policy.ModeEnforce,
		ProposalsEnabled:    false,
		AgentBriefingEnabled: false,
	}
	stored := map[string]json.RawMessage{
		SettingAgentExecMode:     json.RawMessage(`"paper_auto"`),
		SettingTradingHalt:     json.RawMessage(`true`),
		SettingProposalsEnabled: json.RawMessage(`true`),
	}
	out, err := OverlayAppSettings(base, stored)
	if err != nil {
		t.Fatal(err)
	}
	if out.AgentExecMode != AgentExecModePaperAuto {
		t.Fatalf("exec mode: got %q", out.AgentExecMode)
	}
	if !out.TradingHalt {
		t.Fatal("expected trading halt")
	}
	if !out.ProposalsEnabled {
		t.Fatal("expected proposals enabled")
	}
}

func TestOverlayAppSettings_PaperAutoSuppressedInMonitor(t *testing.T) {
	base := Config{PolicyMode: policy.ModeMonitor}
	stored := map[string]json.RawMessage{
		SettingAgentExecMode: json.RawMessage(`"paper_auto"`),
	}
	out, err := OverlayAppSettings(base, stored)
	if err != nil {
		t.Fatal(err)
	}
	if out.AgentExecMode != AgentExecModeOff {
		t.Fatalf("exec mode: got %q", out.AgentExecMode)
	}
	if !out.AgentExecPaperAutoSuppressedDueToMonitorPolicy {
		t.Fatal("expected suppression flag")
	}
}
