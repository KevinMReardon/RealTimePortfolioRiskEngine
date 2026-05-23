package proposals

import (
	"encoding/json"
	"testing"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
)

func TestPolicyResultAllowsAutoSubmit(t *testing.T) {
	t.Parallel()
	allow, _ := json.Marshal(PolicyResultRecord{
		StrictOutcome:    policy.OutcomeAllow,
		EffectiveOutcome: policy.OutcomeAllow,
		PolicyMode:       policy.ModeEnforce,
	})
	if !PolicyResultAllowsAutoSubmit(allow) {
		t.Fatal("expected allow")
	}
	deny, _ := json.Marshal(PolicyResultRecord{
		StrictOutcome:    policy.OutcomeDeny,
		EffectiveOutcome: policy.OutcomeAllow,
		PolicyMode:       policy.ModeMonitor,
	})
	if PolicyResultAllowsAutoSubmit(deny) {
		t.Fatal("monitor effective allow must not auto-submit")
	}
}
