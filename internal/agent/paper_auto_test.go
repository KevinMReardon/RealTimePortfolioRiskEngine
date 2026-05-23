package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

type paperAutoStore struct {
	prop proposals.Proposal
}

func (s *paperAutoStore) GetByIDForPortfolio(ctx context.Context, portfolioID, proposalID uuid.UUID) (proposals.Proposal, error) {
	return s.prop, nil
}

func (s *paperAutoStore) SaveCriticVerdict(ctx context.Context, p proposals.SaveCriticVerdictParams) error {
	return nil
}

func (s *paperAutoStore) ApproveProposalAuto(ctx context.Context, p proposals.AutoApproveParams) error {
	return nil
}

type stubPaperKeys struct {
	material events.PortfolioAlpacaKeyMaterial
	linked   bool
}

func (s stubPaperKeys) LoadPortfolioAlpacaKeyMaterial(ctx context.Context, portfolioID uuid.UUID) (events.PortfolioAlpacaKeyMaterial, bool, error) {
	return s.material, s.linked, nil
}

func TestPaperAutoRunner_SkipsLiveAccount(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	propID := uuid.New()
	allowJSON, _ := json.Marshal(proposals.PolicyResultRecord{
		StrictOutcome: policy.OutcomeAllow, EffectiveOutcome: policy.OutcomeAllow, PolicyMode: policy.ModeEnforce,
	})
	store := &paperAutoStore{prop: proposals.Proposal{
		ProposalID: propID, PortfolioID: pid, Status: "proposed",
		PolicyResult: allowJSON,
	}}
	runner := &PaperAutoRunner{
		Config: PaperAutoConfig{Enabled: true, MaxSubmitsPerSession: 1},
		Keys: stubPaperKeys{linked: true, material: events.PortfolioAlpacaKeyMaterial{AccountMode: "live"}},
		Store:  store,
		Log:    zap.NewNop(),
	}
	runner.RunAfterMaterialize(context.Background(), pid, []uuid.UUID{propID})
	if store.prop.Status != "proposed" {
		t.Fatalf("status=%q want proposed", store.prop.Status)
	}
}
