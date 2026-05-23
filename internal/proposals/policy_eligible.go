package proposals

import (
	"encoding/json"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
)

// PolicyResultAllowsAutoSubmit reports whether stored policy_result permits Phase 3 paper auto-submit.
// Requires strict ALLOW (monitor mode effective ALLOW is not sufficient for autonomy).
func PolicyResultAllowsAutoSubmit(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var rec PolicyResultRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return false
	}
	if rec.PolicyMode == policy.ModeMonitor {
		return false
	}
	return rec.StrictOutcome == policy.OutcomeAllow
}
