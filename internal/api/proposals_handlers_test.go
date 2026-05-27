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

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

func testRuntimeConfigHolder() *config.ConfigHolder {
	return config.NewConfigHolder(config.Config{
		PolicyMode: policy.ModeEnforce,
	})
}

type fakeAlpacaKeyLoader struct {
	linked bool
	err    error
}

func (f *fakeAlpacaKeyLoader) LoadPortfolioAlpacaKeyMaterial(ctx context.Context, portfolioID uuid.UUID) (events.PortfolioAlpacaKeyMaterial, bool, error) {
	if f.err != nil {
		return events.PortfolioAlpacaKeyMaterial{}, false, f.err
	}
	if !f.linked {
		return events.PortfolioAlpacaKeyMaterial{}, false, nil
	}
	return events.PortfolioAlpacaKeyMaterial{
		KeyID:       "k",
		SecretKey:   "s",
		BaseURL:     "https://paper-api.alpaca.markets",
		AccountMode: "paper",
	}, true, nil
}

func TestProposalSubmit_Unauthorized(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	pool := newAPIIntegrationPool(t)
	propStore := proposals.NewStore(pool)
	user := events.UserAccount{
		UserID:       uuid.New(),
		DisplayName:  "User",
		WorkEmail:    "user@example.com",
		PasswordHash: "hash",
	}
	store := newFakeAuthStore()
	_, _ = store.CreateUser(context.Background(), user)
	portfolioID := uuid.New()
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         &fakeOwnedPortfolioReadStore{fakePortfolioReadStore: fakePortfolioReadStore{found: true}, owned: true},
		PriceStreamPartitions: testPricePartitions,
		AuthStore:             store,
		AuthConfig:            AuthConfig{CookieSecure: false, SessionTTL: time.Hour},
		ProposalsStore:        propStore,
		ProposalAlpacaKeys:    &fakeAlpacaKeyLoader{linked: false},
		RuntimeConfig: testRuntimeConfigHolder(),
	})
	propID := uuid.New()
	body := []byte(`{"payload_hash":"abc","row_version":1}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+portfolioID.String()+"/proposals/"+propID.String()+"/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProposalSubmit_InvalidJSON(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	pool := newAPIIntegrationPool(t)
	propStore := proposals.NewStore(pool)
	user := events.UserAccount{
		UserID:       uuid.New(),
		DisplayName:  "User",
		WorkEmail:    "user2@example.com",
		PasswordHash: "hash",
	}
	authSt := newFakeAuthStore()
	_, _ = authSt.CreateUser(context.Background(), user)
	sid := uuid.New()
	_, _ = authSt.CreateSession(context.Background(), events.UserSession{
		SessionID: sid, UserID: user.UserID, ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	portfolioID := uuid.New()
	propID := uuid.New()
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         &fakeOwnedPortfolioReadStore{fakePortfolioReadStore: fakePortfolioReadStore{found: true}, owned: true},
		PriceStreamPartitions: testPricePartitions,
		AuthStore:             authSt,
		AuthConfig:            AuthConfig{CookieSecure: false, SessionTTL: time.Hour},
		ProposalsStore:        propStore,
		ProposalAlpacaKeys:    &fakeAlpacaKeyLoader{linked: false},
		RuntimeConfig: testRuntimeConfigHolder(),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+portfolioID.String()+"/proposals/"+propID.String()+"/submit", bytes.NewReader([]byte(`not-json`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: sid.String()})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.ErrorCode != ErrCodeValidation {
		t.Fatalf("error_code=%s want %s", env.ErrorCode, ErrCodeValidation)
	}
}
