package policy_test

import (
	"testing"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
)

func TestHumanApprovalBlocked(t *testing.T) {
	t.Parallel()
	if policy.HumanApprovalBlocked([]policy.Violation{{Code: policy.RuleMaxOrderNotional}}) {
		t.Fatal("max order notional should not block human approval")
	}
	if policy.HumanApprovalBlocked([]policy.Violation{{Code: policy.RuleMarketHours}}) {
		t.Fatal("market hours should not block human approval")
	}
	if !policy.HumanApprovalBlocked([]policy.Violation{{Code: policy.RuleKillSwitch}}) {
		t.Fatal("kill switch should block human approval")
	}
	if !policy.HumanApprovalBlocked([]policy.Violation{
		{Code: policy.RuleMaxOrderNotional},
		{Code: policy.RuleSymbolBlacklist},
	}) {
		t.Fatal("blacklist should block human approval")
	}
}
