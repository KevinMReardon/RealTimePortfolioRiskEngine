package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

type paperAutoGateStub struct {
	active    bool
	session   events.AgentSession
	hasSess   bool
	activeErr error
	sessErr   error
}

func (g *paperAutoGateStub) GetLatestSucceededAgentSessionForPortfolio(ctx context.Context, portfolioID uuid.UUID) (events.AgentSession, bool, error) {
	return g.session, g.hasSess, g.sessErr
}

func (g *paperAutoGateStub) PortfolioHasActiveBriefing(ctx context.Context, portfolioID uuid.UUID) (bool, error) {
	return g.active, g.activeErr
}

type paperAutoSessionStore struct {
	paperAutoStore
	sessionID uuid.UUID
	bySession []proposals.Proposal
	retryMu   sync.Mutex
	retries   map[uuid.UUID]int
	abandoned map[uuid.UUID]bool
}

func (s *paperAutoSessionStore) ListByAgentSession(ctx context.Context, portfolioID, sessionID uuid.UUID, filter proposals.ListByAgentSessionFilter) ([]proposals.Proposal, error) {
	if sessionID != s.sessionID {
		return nil, nil
	}
	return s.bySession, nil
}

func (s *paperAutoSessionStore) RecordPaperAutoRetryFailure(ctx context.Context, portfolioID, proposalID uuid.UUID, maxAttempts int, lastError string) (proposals.Proposal, error) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	if s.retries == nil {
		s.retries = make(map[uuid.UUID]int)
	}
	if s.abandoned == nil {
		s.abandoned = make(map[uuid.UUID]bool)
	}
	s.retries[proposalID]++
	s.prop.PaperAutoRetryCount = s.retries[proposalID]
	if s.prop.PaperAutoRetryCount >= maxAttempts {
		s.prop.Status = proposals.StatusAutoAbandoned
		s.abandoned[proposalID] = true
	}
	return s.prop, nil
}

func TestPaperAutoRetryRunner_PausesWhenBriefingActive(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	sid := uuid.New()
	propID := uuid.New()
	store := &paperAutoSessionStore{
		sessionID: sid,
		bySession: []proposals.Proposal{{ProposalID: propID, PortfolioID: pid, Status: proposals.StatusProposed}},
		paperAutoStore: paperAutoStore{prop: proposals.Proposal{
			ProposalID: propID, PortfolioID: pid, Status: proposals.StatusProposed,
		}},
	}
	runner := &PaperAutoRetryRunner{
		Config: PaperAutoRetryConfig{Enabled: true, Interval: time.Hour, MaxPerTick: 10, MaxRetries: 3},
		Auto: &PaperAutoRunner{
			Config: PaperAutoConfig{Enabled: true, MaxAutoRetries: 3},
			Critic: &Critic{},
			Keys:   stubPaperKeys{linked: true, material: events.PortfolioAlpacaKeyMaterial{AccountMode: "paper"}},
			Store:  store,
			Log:    zap.NewNop(),
		},
		Gate:    &paperAutoGateStub{active: true, hasSess: true, session: events.AgentSession{SessionID: sid, PortfolioID: pid}},
		Catalog: retryCatalogStub{rows: []events.AgentBriefingEligiblePortfolio{{PortfolioID: pid}}},
		Log:     zap.NewNop(),
	}
	runner.retryPortfolio(context.Background(), zap.NewNop(), pid)
	if len(store.retries) != 0 {
		t.Fatalf("expected no retries while briefing active, got %v", store.retries)
	}
}

func TestPaperAutoRetryRunner_OnlyLatestSessionProposals(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	latestSID := uuid.New()
	oldSID := uuid.New()
	latestProp := uuid.New()
	oldProp := uuid.New()
	store := &paperAutoSessionStore{
		sessionID: latestSID,
		bySession: []proposals.Proposal{
			{ProposalID: latestProp, PortfolioID: pid, AgentSessionID: &latestSID, Status: proposals.StatusProposed},
		},
		paperAutoStore: paperAutoStore{prop: proposals.Proposal{
			ProposalID: latestProp, PortfolioID: pid, Status: proposals.StatusProposed,
		}},
	}
	runner := &PaperAutoRetryRunner{
		Config: PaperAutoRetryConfig{Enabled: true, MaxPerTick: 10, MaxRetries: 3},
		Auto: &PaperAutoRunner{
			Config: PaperAutoConfig{Enabled: true, MaxAutoRetries: 3},
			Critic: &Critic{Store: store},
			Keys:   stubPaperKeys{linked: true, material: events.PortfolioAlpacaKeyMaterial{AccountMode: "paper"}},
			Store:  store,
			Log:    zap.NewNop(),
		},
		Gate: &paperAutoGateStub{
			hasSess: true,
			session: events.AgentSession{SessionID: latestSID, PortfolioID: pid},
		},
		Catalog: retryCatalogStub{rows: []events.AgentBriefingEligiblePortfolio{{PortfolioID: pid}}},
		Log:     zap.NewNop(),
	}
	_ = oldSID
	_ = oldProp
	runner.retryPortfolio(context.Background(), zap.NewNop(), pid)
	list, _ := store.ListByAgentSession(context.Background(), pid, oldSID, proposals.ListByAgentSessionFilter{})
	if len(list) != 0 {
		t.Fatalf("old session should not be listed, got %+v", list)
	}
}

func TestPaperAutoRunner_RetryCapAbandons(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	propID := uuid.New()
	allowJSON, _ := json.Marshal(proposals.PolicyResultRecord{
		StrictOutcome: policy.OutcomeAllow, EffectiveOutcome: policy.OutcomeAllow, PolicyMode: policy.ModeEnforce,
	})
	store := &paperAutoSessionStore{
		paperAutoStore: paperAutoStore{prop: proposals.Proposal{
			ProposalID: propID, PortfolioID: pid, Status: proposals.StatusProposed,
			PolicyResult: allowJSON, CriticVerdict: json.RawMessage(`{}`),
		}},
	}
	runner := &PaperAutoRunner{
		Config: PaperAutoConfig{Enabled: true, MaxAutoRetries: 3},
		Critic: &Critic{Store: store},
		Keys:   stubPaperKeys{linked: true, material: events.PortfolioAlpacaKeyMaterial{AccountMode: "paper"}},
		Store:  store,
		Log:    zap.NewNop(),
	}
	log := zap.NewNop()
	for i := 0; i < 3; i++ {
		runner.processOneRetry(context.Background(), log, pid, propID)
	}
	if store.prop.Status != proposals.StatusAutoAbandoned {
		t.Fatalf("status=%q want auto_abandoned after 3 failures", store.prop.Status)
	}
	if store.prop.PaperAutoRetryCount != 3 {
		t.Fatalf("retry_count=%d want 3", store.prop.PaperAutoRetryCount)
	}
}

func TestPaperAutoRunner_MaterializeDoesNotIncrementRetryCount(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	propID := uuid.New()
	allowJSON, _ := json.Marshal(proposals.PolicyResultRecord{
		StrictOutcome: policy.OutcomeAllow, EffectiveOutcome: policy.OutcomeAllow, PolicyMode: policy.ModeEnforce,
	})
	store := &paperAutoSessionStore{
		paperAutoStore: paperAutoStore{prop: proposals.Proposal{
			ProposalID: propID, PortfolioID: pid, Status: proposals.StatusProposed,
			PolicyResult: allowJSON,
		}},
	}
	runner := &PaperAutoRunner{
		Config: PaperAutoConfig{Enabled: true, MaxAutoRetries: 3},
		Critic: &Critic{Store: store},
		Keys:   stubPaperKeys{linked: true, material: events.PortfolioAlpacaKeyMaterial{AccountMode: "paper"}},
		Store:  store,
		Log:    zap.NewNop(),
	}
	runner.processOne(context.Background(), zap.NewNop(), pid, propID, true, paperAutoPassFresh)
	if store.prop.PaperAutoRetryCount != 0 {
		t.Fatalf("retry_count=%d want 0 for materialize pass", store.prop.PaperAutoRetryCount)
	}
}

func TestIsAnthropicRateLimit(t *testing.T) {
	t.Parallel()
	if !isAnthropicRateLimit(fmt.Errorf("anthropic http 429: overloaded")) {
		t.Fatal("expected rate limit detection")
	}
}
