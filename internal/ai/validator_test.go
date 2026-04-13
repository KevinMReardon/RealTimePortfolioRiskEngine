package ai

import (
	"errors"
	"testing"
)

func allowAAPL() map[string]struct{} {
	return map[string]struct{}{"AAPL": {}}
}

func TestValidateInsightOutput_okPlain(t *testing.T) {
	t.Parallel()
	err := ValidateInsightOutput(allowAAPL(), `{"narrative":"AAPL exposure is listed in risk.exposure.","used_metrics":["risk.exposure"]}`, []string{"risk.exposure"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateInsightOutput_bannedPhrase(t *testing.T) {
	t.Parallel()
	err := ValidateInsightOutput(allowAAPL(), `I updated your portfolio today.`, nil)
	var v *ValidationError
	if !errors.As(err, &v) || v.Code != ValReasonBannedPhrase {
		t.Fatalf("got %v", err)
	}
}

func TestValidateInsightOutput_unknownSymbol(t *testing.T) {
	t.Parallel()
	err := ValidateInsightOutput(allowAAPL(), `MSFT looks interesting per risk metrics.`, nil)
	var v *ValidationError
	if !errors.As(err, &v) || v.Code != ValReasonUnknownSymbol {
		t.Fatalf("got %v", err)
	}
}

func TestValidateInsightOutput_allowsEnglishCapsStoplist(t *testing.T) {
	t.Parallel()
	err := ValidateInsightOutput(allowAAPL(), `OR you could note IT is not a ticker here.`, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateInsightOutput_allowsFinanceStoplist(t *testing.T) {
	t.Parallel()
	err := ValidateInsightOutput(allowAAPL(), `CASH and USD are mentioned alongside AAPL.`, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateInsightOutput_suspiciousSQL(t *testing.T) {
	t.Parallel()
	err := ValidateInsightOutput(allowAAPL(), `UNION SELECT * FROM users`, nil)
	var v *ValidationError
	if !errors.As(err, &v) || v.Code != ValReasonSuspiciousSQL {
		t.Fatalf("got %v", err)
	}
}

func TestValidateInsightOutput_badUsedMetric(t *testing.T) {
	t.Parallel()
	err := ValidateInsightOutput(allowAAPL(), `{}`, []string{"evil.path"})
	var v *ValidationError
	if !errors.As(err, &v) || v.Code != ValReasonBadUsedMetric {
		t.Fatalf("got %v", err)
	}
}

func TestValidateInsightOutput_emptyText(t *testing.T) {
	t.Parallel()
	if err := ValidateInsightOutput(allowAAPL(), "", nil); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeUnknownTickerTokens_rewritesOnlyUnknown(t *testing.T) {
	t.Parallel()
	in := `{"narrative":"AAPL moved while HHI lagged. USD cash unchanged.","used_metrics":["risk.exposure"]}`
	got := SanitizeUnknownTickerTokens(in, allowAAPL())
	if got == in {
		t.Fatal("expected rewrite for unknown token")
	}
	if err := ValidateInsightOutput(allowAAPL(), got, []string{"risk.exposure"}); err != nil {
		t.Fatalf("sanitized output should validate: %v", err)
	}
}
