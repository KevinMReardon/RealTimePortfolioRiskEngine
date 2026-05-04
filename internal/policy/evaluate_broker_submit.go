package policy

// EvaluateForBrokerSubmit re-runs policy for placing a broker order after the proposal was
// human-approved. Risk-budget rules that already produced a materialized proposal (and were
// visible to the approver) are not re-applied as hard blocks; operational and safety gates remain.
//
// US regular session (MARKET_HOURS) is not a hard gate here: the approver already accepted the idea
// and the broker API enforces what is actually executable (extended hours, queued orders, rejects).
func EvaluateForBrokerSubmit(intent Intent, snap Snapshot, cfg Config) Decision {
	full := evaluateCore(intent, snap, cfg)
	hard := make([]Violation, 0, len(full.Violations))
	for _, v := range full.Violations {
		if isBrokerSubmitHardGate(v.Code) {
			hard = append(hard, v)
		}
	}
	out := full
	out.Violations = hard
	if len(hard) > 0 {
		out.StrictOutcome = OutcomeDeny
	} else {
		out.StrictOutcome = OutcomeAllow
	}
	out.EffectiveOutcome = out.StrictOutcome
	if cfg.Mode == ModeMonitor && out.StrictOutcome == OutcomeDeny {
		out.EffectiveOutcome = OutcomeAllow
	}
	observePolicyEvaluationResult(out, snap)
	return out
}

func isBrokerSubmitHardGate(code string) bool {
	switch code {
	case RuleKillSwitch,
		RuleDataMarkPrice,
		RuleDataEquity,
		RuleIntentSymbol,
		RuleIntentSide,
		RuleTradingBlocked,
		RuleAccountBlocked,
		RuleSymbolBlacklist,
		RulePDTUnder25k:
		return true
	default:
		return false
	}
}
