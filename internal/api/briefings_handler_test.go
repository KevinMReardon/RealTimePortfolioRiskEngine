package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/agent"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

type fakeAgentService struct {
	onDemand      func(ctx context.Context, req agent.RunBriefingRequest) (agent.RunBriefingResult, error)
	scheduledRuns int
	latest        events.AgentSession
	found         bool
	replay        events.AgentSessionReplay
}

func (f *fakeAgentService) RunBriefing(ctx context.Context, req agent.RunBriefingRequest) (agent.RunBriefingResult, error) {
	return f.CreateBriefingOnDemand(ctx, req)
}
func (f *fakeAgentService) CreateBriefingOnDemand(ctx context.Context, req agent.RunBriefingRequest) (agent.RunBriefingResult, error) {
	if f.onDemand != nil {
		return f.onDemand(ctx, req)
	}
	return agent.RunBriefingResult{}, nil
}
func (f *fakeAgentService) CreateBriefingScheduled(ctx context.Context, req agent.RunBriefingRequest) (agent.RunBriefingResult, error) {
	f.scheduledRuns++
	return f.CreateBriefingOnDemand(ctx, req)
}
func (f *fakeAgentService) GetLatestBriefing(context.Context, uuid.UUID) (events.AgentSession, bool, error) {
	return f.latest, f.found, nil
}
func (f *fakeAgentService) ListBriefings(context.Context, uuid.UUID, events.AgentSessionListFilter) ([]events.AgentSession, error) {
	return []events.AgentSession{f.latest}, nil
}
func (f *fakeAgentService) GetSessionReplay(context.Context, uuid.UUID) (events.AgentSessionReplay, bool, error) {
	return f.replay, true, nil
}
func (f *fakeAgentService) GetReplay(ctx context.Context, sessionID uuid.UUID) (events.AgentSessionReplay, bool, error) {
	return f.GetSessionReplay(ctx, sessionID)
}
func (f *fakeAgentService) ListPortfolioSessions(ctx context.Context, portfolioID uuid.UUID, filter events.AgentSessionListFilter) ([]events.AgentSession, error) {
	return f.ListBriefings(ctx, portfolioID, filter)
}

func TestBriefingsPost_ResponseContract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	user := events.UserAccount{
		UserID:       uuid.New(),
		DisplayName:  "User",
		WorkEmail:    "user@example.com",
		PasswordHash: "hash",
	}
	store := newFakeAuthStore()
	_, _ = store.CreateUser(context.Background(), user)
	sid := uuid.New()
	_, _ = store.CreateSession(context.Background(), events.UserSession{
		SessionID: sid, UserID: user.UserID, ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	portfolioID := uuid.New()
	agentSvc := &fakeAgentService{
		onDemand: func(ctx context.Context, req agent.RunBriefingRequest) (agent.RunBriefingResult, error) {
			if req.MaxTokens == nil || *req.MaxTokens != 4096 {
				t.Fatalf("MaxTokens = %v, want 4096 default from router config", req.MaxTokens)
			}
			if req.Temperature == nil || *req.Temperature != 0.15 {
				t.Fatalf("Temperature = %v, want 0.15 default from router config", req.Temperature)
			}
			return agent.RunBriefingResult{
				Session: events.AgentSession{SessionID: uuid.New(), Status: "succeeded"},
				Output: agent.BriefingOutput{
					MarketSummary:    "summary",
					PortfolioContext: "context",
					TradeIdeas: []agent.BriefingIdea{
						{Symbol: "AAPL", Rationale: "r", Confidence: 0.8, Size: "small", Stop: "s", Target: "t"},
					},
					RisksAndCaveats: "risk",
					DataGaps:        []string{},
					Disclaimer:      "disc",
					UsedSources:     []string{"src"},
					UsedFields:      []string{"field"},
				},
			}, nil
		},
	}
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         &fakeOwnedPortfolioReadStore{fakePortfolioReadStore: fakePortfolioReadStore{found: true}, owned: true},
		PortfolioCatalog:      &fakePortfolioCatalogStore{ownershipOK: true, ownershipSet: true},
		PriceStreamPartitions: testPricePartitions,
		AuthStore:             store,
		AuthConfig:            AuthConfig{CookieSecure: false, SessionTTL: time.Hour},
		AgentService:          agentSvc,
		AgentMaxTokens:        4096,
		AgentTemperature:      0.15,
	})
	body := []byte(`{"user_input":{"question":"summarize"}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+portfolioID.String()+"/briefings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: sid.String()})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["session_id"] == "" || payload["status"] != "succeeded" {
		t.Fatalf("unexpected response: %#v", payload)
	}
	output, ok := payload["output"].(map[string]any)
	if !ok {
		t.Fatalf("missing output object: %#v", payload)
	}
	if _, ok := output["trade_ideas"]; !ok {
		t.Fatalf("missing trade_ideas in output: %#v", output)
	}
}

func TestBriefingsPost_ScheduledFlagUsesManualPath(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	user := events.UserAccount{
		UserID:       uuid.New(),
		DisplayName:  "User",
		WorkEmail:    "scheduled-compat@example.com",
		PasswordHash: "hash",
	}
	store := newFakeAuthStore()
	_, _ = store.CreateUser(context.Background(), user)
	sid := uuid.New()
	_, _ = store.CreateSession(context.Background(), events.UserSession{
		SessionID: sid, UserID: user.UserID, ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	portfolioID := uuid.New()
	agentSvc := &fakeAgentService{
		onDemand: func(ctx context.Context, req agent.RunBriefingRequest) (agent.RunBriefingResult, error) {
			if req.TriggerSource == "scheduled" {
				t.Fatalf("API scheduled=true must not use cron scheduled trigger")
			}
			var input map[string]any
			if err := json.Unmarshal(req.UserInput, &input); err != nil {
				t.Fatalf("unmarshal user input: %v", err)
			}
			if input["requested_scheduled"] != true {
				t.Fatalf("expected requested_scheduled marker, got %#v", input)
			}
			return agent.RunBriefingResult{
				Session: events.AgentSession{SessionID: uuid.New(), Status: "succeeded"},
				Output:  agent.BriefingOutput{MarketSummary: "m", PortfolioContext: "p", RisksAndCaveats: "r", Disclaimer: "d"},
			}, nil
		},
	}
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         &fakeOwnedPortfolioReadStore{fakePortfolioReadStore: fakePortfolioReadStore{found: true}, owned: true},
		PortfolioCatalog:      &fakePortfolioCatalogStore{ownershipOK: true, ownershipSet: true},
		PriceStreamPartitions: testPricePartitions,
		AuthStore:             store,
		AuthConfig:            AuthConfig{CookieSecure: false, SessionTTL: time.Hour},
		AgentService:          agentSvc,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+portfolioID.String()+"/briefings", bytes.NewReader([]byte(`{"scheduled":true}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: sid.String()})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if agentSvc.scheduledRuns != 0 {
		t.Fatalf("expected CreateBriefingScheduled not to be called, calls=%d", agentSvc.scheduledRuns)
	}
}

func TestBriefingsReplay_ForbiddenWhenNotOwner(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	user := events.UserAccount{
		UserID:       uuid.New(),
		DisplayName:  "User",
		WorkEmail:    "user2@example.com",
		PasswordHash: "hash",
	}
	store := newFakeAuthStore()
	_, _ = store.CreateUser(context.Background(), user)
	sid := uuid.New()
	_, _ = store.CreateSession(context.Background(), events.UserSession{
		SessionID: sid, UserID: user.UserID, ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	agentSvc := &fakeAgentService{
		replay: events.AgentSessionReplay{
			Session: events.AgentSession{SessionID: uuid.New(), PortfolioID: uuid.New()},
		},
	}
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         &fakeOwnedPortfolioReadStore{fakePortfolioReadStore: fakePortfolioReadStore{found: true}, owned: false},
		PortfolioCatalog:      &fakePortfolioCatalogStore{ownershipOK: false, ownershipSet: true},
		PriceStreamPartitions: testPricePartitions,
		AuthStore:             store,
		AuthConfig:            AuthConfig{CookieSecure: false, SessionTTL: time.Hour},
		AgentService:          agentSvc,
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/agent-sessions/"+uuid.New().String()+"/replay", nil)
	req.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: sid.String()})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
