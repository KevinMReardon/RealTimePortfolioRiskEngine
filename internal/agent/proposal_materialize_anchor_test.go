package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

type materializeAnchorStore struct {
	anchors map[string]decimal.Decimal
}

func (s *materializeAnchorStore) InsertProposal(ctx context.Context, p proposals.InsertParams) (proposals.Proposal, error) {
	return proposals.Proposal{}, nil
}

func (s *materializeAnchorStore) LoadEquityAnchorForPortfolioDate(_ context.Context, portfolioID uuid.UUID, anchorDate time.Time) (decimal.Decimal, bool, error) {
	d := time.Date(anchorDate.Year(), anchorDate.Month(), anchorDate.Day(), 0, 0, 0, 0, time.UTC)
	eq, ok := s.anchors[portfolioID.String()+"|"+d.Format("2006-01-02")]
	return eq, ok, nil
}

func (s *materializeAnchorStore) KillSwitchLatestActive(context.Context) (bool, bool, error) {
	return false, false, nil
}

type stubAnchorEnsurer struct {
	store *materializeAnchorStore
	eq    decimal.Decimal
}

func (s *stubAnchorEnsurer) EnsureTodayForPortfolioKeys(_ context.Context, portfolioID uuid.UUID, _ events.PortfolioAlpacaKeyMaterial) {
	if s.store.anchors == nil {
		s.store.anchors = make(map[string]decimal.Decimal)
	}
	d := todayAnchorDateUTC(time.Now())
	s.store.anchors[portfolioID.String()+"|"+d.Format("2006-01-02")] = s.eq
}

type materializeKeyLoader struct {
	keys events.PortfolioAlpacaKeyMaterial
}

func (m materializeKeyLoader) LoadPortfolioAlpacaKeyMaterial(context.Context, uuid.UUID) (events.PortfolioAlpacaKeyMaterial, bool, error) {
	return m.keys, true, nil
}

type materializeLoader struct {
	in portfolio.PortfolioAssemblerInput
}

func (m materializeLoader) LoadPortfolioAssemblerInput(context.Context, uuid.UUID) (portfolio.PortfolioAssemblerInput, bool, error) {
	return m.in, true, nil
}

func TestBriefingProposalMaterializer_EnsureBeforePolicyLimits(t *testing.T) {
	pid := uuid.New()
	store := &materializeAnchorStore{anchors: make(map[string]decimal.Decimal)}
	mat := &BriefingProposalMaterializer{
		Store: store,
		Loader: materializeLoader{in: portfolio.PortfolioAssemblerInput{PortfolioID: pid}},
		Keys: materializeKeyLoader{keys: events.PortfolioAlpacaKeyMaterial{
			KeyID: "k", SecretKey: "s", AccountMode: "paper",
		}},
		AnchorEnsurer: &stubAnchorEnsurer{store: store, eq: decimal.RequireFromString("100500")},
		Policy:        policy.Config{MaxDailyLossPct: decimal.RequireFromString("2")},
		Log:           zap.NewNop(),
	}
	limits := mat.PolicyLimitsForPortfolio(context.Background(), pid)
	if limits == nil {
		t.Fatal("expected policy limits JSON")
	}
	anchorDate := todayAnchorDateUTC(time.Now())
	eq, ok, _ := store.LoadEquityAnchorForPortfolioDate(context.Background(), pid, anchorDate)
	if !ok || !eq.IsPositive() {
		t.Fatalf("expected positive anchor after ensure, got %s ok=%v", eq.String(), ok)
	}
}
