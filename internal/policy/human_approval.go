package policy

// HumanApprovalBlocked reports whether human approval must be denied in enforce mode.
// Soft risk-budget violations (max notional, position %, market hours, etc.) return false so
// humans can approve and submit via EvaluateForBrokerSubmit; paper_auto still requires strict ALLOW.
func HumanApprovalBlocked(violations []Violation) bool {
	for _, v := range violations {
		if isBrokerSubmitHardGate(v.Code) {
			return true
		}
	}
	return false
}
