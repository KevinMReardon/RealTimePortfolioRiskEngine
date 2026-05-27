package recon

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

type targetListerStub struct {
	targets []Target
}

func (t targetListerStub) ListTargets(ctx context.Context) ([]Target, error) {
	return t.targets, nil
}

type storeStub struct {
	props []proposals.Proposal

	filled    []uuid.UUID
	cancelled []uuid.UUID
}

func (s *storeStub) ListByPortfolio(ctx context.Context, portfolioID uuid.UUID, filter proposals.ListFilter) ([]proposals.Proposal, error) {
	return s.props, nil
}

func (s *storeStub) MarkProposalFilled(ctx context.Context, portfolioID, proposalID uuid.UUID, brokerOrderID string) error {
	s.filled = append(s.filled, proposalID)
	return nil
}

func (s *storeStub) MarkProposalCancelled(ctx context.Context, portfolioID, proposalID uuid.UUID, brokerOrderID, reason string) error {
	s.cancelled = append(s.cancelled, proposalID)
	return nil
}

func TestWorker_TickUpdatesProposalStatuses(t *testing.T) {
	pid := uuid.New()
	pFilled := uuid.New()
	pCancelled := uuid.New()
	bFilled := "ord-filled"
	bCancelled := "ord-cancelled"
	st := &storeStub{
		props: []proposals.Proposal{
			{ProposalID: pFilled, PortfolioID: pid, BrokerOrderID: &bFilled},
			{ProposalID: pCancelled, PortfolioID: pid, BrokerOrderID: &bCancelled},
		},
	}
	rest := &alpaca.FakeREST{
		Orders: []alpaca.OrderSnapshot{
			{ID: bFilled, Status: "filled"},
			{ID: bCancelled, Status: "canceled"},
		},
	}
	w := &Worker{
		Store:   st,
		Targets: targetListerStub{targets: []Target{{PortfolioID: pid, KeyID: "k", SecretKey: "s", BaseURL: "https://paper-api.alpaca.markets"}}},
		NewREST: func(cfg alpaca.RESTConfig) (alpaca.REST, error) { return rest, nil },
		Interval: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	w.Run(ctx)
	if len(st.filled) == 0 {
		t.Fatalf("expected filled update")
	}
	if len(st.cancelled) == 0 {
		t.Fatalf("expected cancelled update")
	}
}
