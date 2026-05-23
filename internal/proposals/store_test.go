package proposals

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestKillSwitchInputs(t *testing.T) {
	t.Parallel()
	e, d := KillSwitchInputs(true, false, false)
	if !e || d {
		t.Fatalf("want env only: got env=%v db=%v", e, d)
	}
	e, d = KillSwitchInputs(false, true, true)
	if e || !d {
		t.Fatalf("want db only: got env=%v db=%v", e, d)
	}
	e, d = KillSwitchInputs(false, true, false) // row absent => db flag false
	if e || d {
		t.Fatalf("want no db when absent: got env=%v db=%v", e, d)
	}
}

func TestSaveCriticVerdict_validation(t *testing.T) {
	t.Parallel()
	s := NewStore(nil)
	err := s.SaveCriticVerdict(context.Background(), SaveCriticVerdictParams{
		PortfolioID: uuid.New(),
		ProposalID:  uuid.New(),
		Verdict:     nil,
		CompletedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for nil verdict")
	}
}
