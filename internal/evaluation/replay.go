package evaluation

import (
	"encoding/json"
	"os"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

// ReplayFixture is a minimal session replay for offline evaluation.
type ReplayFixture struct {
	Proposals []proposals.PolicyResultRecord `json:"proposals"`
	// PeriodReturns optional equity curve inputs (decimal strings).
	PeriodReturns []string `json:"period_returns"`
}

// LoadReplayFixture reads JSON from path.
func LoadReplayFixture(path string) (ReplayFixture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ReplayFixture{}, err
	}
	var f ReplayFixture
	if err := json.Unmarshal(b, &f); err != nil {
		return ReplayFixture{}, err
	}
	return f, nil
}

// CountPolicyViolations counts proposals whose strict outcome is DENY.
func CountPolicyViolations(recs []proposals.PolicyResultRecord) int {
	n := 0
	for _, r := range recs {
		if r.StrictOutcome == policy.OutcomeDeny {
			n++
		}
	}
	return n
}
