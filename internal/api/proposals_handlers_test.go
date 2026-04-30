package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

func TestProposalSubmit_NotImplemented(t *testing.T) {
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
	var stub proposals.Store
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         &fakeOwnedPortfolioReadStore{fakePortfolioReadStore: fakePortfolioReadStore{found: true}, owned: true},
		PriceStreamPartitions: testPricePartitions,
		AuthStore:             store,
		AuthConfig:            AuthConfig{CookieSecure: false, SessionTTL: time.Hour},
		ProposalsStore:        &stub,
	})
	propID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+portfolioID.String()+"/proposals/"+propID.String()+"/submit", nil)
	req.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: sid.String()})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.ErrorCode != ErrCodeNotImplemented {
		t.Fatalf("error_code=%s want %s", env.ErrorCode, ErrCodeNotImplemented)
	}
}
