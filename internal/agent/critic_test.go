package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

type fakeCriticStore struct {
	prop        proposals.Proposal
	saved       bool
	lastVerdict json.RawMessage
}

type flakyCriticClient struct {
	failuresRemaining int
}

func (f *flakyCriticClient) CreateMessage(_ context.Context, req AnthropicMessageRequest) (AnthropicMessageResponse, error) {
	_ = req
	if f.failuresRemaining > 0 {
		f.failuresRemaining--
		return AnthropicMessageResponse{}, errors.New("temporary provider failure")
	}
	return AnthropicMessageResponse{
		OutputText: `{"allow":true,"reason_code":"ok","notes":"looks fine"}`,
	}, nil
}

type captureCriticRequestClient struct {
	lastReq AnthropicMessageRequest
}

func (c *captureCriticRequestClient) CreateMessage(_ context.Context, req AnthropicMessageRequest) (AnthropicMessageResponse, error) {
	c.lastReq = req
	return AnthropicMessageResponse{
		OutputText: `{"allow":true,"reason_code":"ok","notes":"looks fine"}`,
	}, nil
}

func (f *fakeCriticStore) GetByIDForPortfolio(ctx context.Context, portfolioID, proposalID uuid.UUID) (proposals.Proposal, error) {
	return f.prop, nil
}

func (f *fakeCriticStore) SaveCriticVerdict(ctx context.Context, p proposals.SaveCriticVerdictParams) error {
	f.saved = true
	f.lastVerdict = append(json.RawMessage(nil), p.Verdict...)
	return nil
}

func TestParseCriticVerdict(t *testing.T) {
	t.Parallel()
	v, err := parseCriticVerdict(`{"allow":true,"reason_code":"ok","notes":"fine"}`)
	if err != nil || !v.Allow || v.ReasonCode != "ok" {
		t.Fatalf("got %+v err=%v", v, err)
	}
	v2, err := parseCriticVerdict("Here is the result:\n{\"allow\":false,\"reason_code\":\"risk\",\"notes\":\"no\"}")
	if err != nil || v2.Allow {
		t.Fatalf("got %+v", v2)
	}
	_, err = parseCriticVerdict("not json")
	if err == nil {
		t.Fatal("want error")
	}
}

func TestCriticReview_VetoOnParseFailure(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	propID := uuid.New()
	store := &fakeCriticStore{prop: proposals.Proposal{
		ProposalID:   propID,
		PortfolioID:  pid,
		Symbol:       "AAPL",
		Side:         "BUY",
		PolicyResult: mustPolicyJSON(t, policy.OutcomeAllow),
	}}
	client := &mockAnthropicClient{responses: []AnthropicMessageResponse{{
		OutputText: "not-json",
	}}}
	c := &Critic{Client: client, Store: store, Model: "test"}
	v, err := c.Review(context.Background(), pid, propID)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if v.Allow || !store.saved {
		t.Fatalf("verdict=%+v saved=%v", v, store.saved)
	}
}

func TestCriticReview_RetriesProviderFailures(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	propID := uuid.New()
	store := &fakeCriticStore{prop: proposals.Proposal{
		ProposalID:   propID,
		PortfolioID:  pid,
		Symbol:       "AAPL",
		Side:         "BUY",
		PolicyResult: mustPolicyJSON(t, policy.OutcomeAllow),
	}}
	client := &flakyCriticClient{failuresRemaining: 2}
	c := &Critic{Client: client, Store: store, Model: "test"}
	v, err := c.Review(context.Background(), pid, propID)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if !v.Allow {
		t.Fatalf("expected allow verdict after retries, got %+v", v)
	}
	if !store.saved {
		t.Fatal("expected verdict to be persisted")
	}
}

func TestCriticReview_PersistsProviderErrorVerdict(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	propID := uuid.New()
	store := &fakeCriticStore{prop: proposals.Proposal{
		ProposalID:   propID,
		PortfolioID:  pid,
		Symbol:       "AAPL",
		Side:         "BUY",
		PolicyResult: mustPolicyJSON(t, policy.OutcomeAllow),
	}}
	client := &flakyCriticClient{failuresRemaining: 10}
	c := &Critic{Client: client, Store: store, Model: "test"}
	v, err := c.Review(context.Background(), pid, propID)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if v.ReasonCode != "critic_provider_error" {
		t.Fatalf("reason_code=%q want critic_provider_error", v.ReasonCode)
	}
	if !store.saved {
		t.Fatal("expected provider-error verdict to be persisted")
	}
}

func TestCriticReview_SetsMaxTokensOnProviderRequest(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	propID := uuid.New()
	store := &fakeCriticStore{prop: proposals.Proposal{
		ProposalID:   propID,
		PortfolioID:  pid,
		Symbol:       "AAPL",
		Side:         "BUY",
		PolicyResult: mustPolicyJSON(t, policy.OutcomeAllow),
	}}
	client := &captureCriticRequestClient{}
	c := &Critic{Client: client, Store: store, Model: "test"}
	if _, err := c.Review(context.Background(), pid, propID); err != nil {
		t.Fatalf("Review: %v", err)
	}
	if client.lastReq.MaxTokens == nil || *client.lastReq.MaxTokens != criticMaxTokens {
		t.Fatalf("MaxTokens=%v want %d", client.lastReq.MaxTokens, criticMaxTokens)
	}
}

func mustPolicyJSON(t *testing.T, out policy.Outcome) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(proposals.PolicyResultRecord{
		StrictOutcome:    out,
		EffectiveOutcome: out,
		PolicyMode:       policy.ModeEnforce,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
