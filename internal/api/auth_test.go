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

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

type fakeAuthStore struct {
	usersByEmail map[string]events.UserAccount
	usersByID    map[uuid.UUID]events.UserAccount
	sessions     map[uuid.UUID]events.UserSession
}

type fakeOwnedPortfolioReadStore struct {
	fakePortfolioReadStore
	owned bool
}

func (f *fakeOwnedPortfolioReadStore) PortfolioOwnedByUser(ctx context.Context, portfolioID, ownerUserID uuid.UUID) (bool, error) {
	_ = ctx
	_ = portfolioID
	_ = ownerUserID
	return f.owned, nil
}

func newFakeAuthStore() *fakeAuthStore {
	return &fakeAuthStore{
		usersByEmail: map[string]events.UserAccount{},
		usersByID:    map[uuid.UUID]events.UserAccount{},
		sessions:     map[uuid.UUID]events.UserSession{},
	}
}

func (f *fakeAuthStore) CreateUser(ctx context.Context, user events.UserAccount) (events.UserAccount, error) {
	_ = ctx
	f.usersByEmail[user.WorkEmail] = user
	f.usersByID[user.UserID] = user
	return user, nil
}

func (f *fakeAuthStore) GetUserByEmail(ctx context.Context, workEmail string) (events.UserAccount, bool, error) {
	_ = ctx
	u, ok := f.usersByEmail[workEmail]
	return u, ok, nil
}

func (f *fakeAuthStore) GetUserByID(ctx context.Context, userID uuid.UUID) (events.UserAccount, bool, error) {
	_ = ctx
	u, ok := f.usersByID[userID]
	return u, ok, nil
}

func (f *fakeAuthStore) CreateSession(ctx context.Context, session events.UserSession) (events.UserSession, error) {
	_ = ctx
	f.sessions[session.SessionID] = session
	return session, nil
}

func (f *fakeAuthStore) GetSessionByID(ctx context.Context, sessionID uuid.UUID) (events.UserSession, bool, error) {
	_ = ctx
	s, ok := f.sessions[sessionID]
	return s, ok, nil
}

func (f *fakeAuthStore) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	_ = ctx
	s, ok := f.sessions[sessionID]
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	s.RevokedAt = &now
	f.sessions[sessionID] = s
	return nil
}

func TestAuth_RegisterLoginMeLogout(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	store := newFakeAuthStore()
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         &fakePortfolioReadStore{found: false},
		PortfolioCatalog:      &fakePortfolioCatalogStore{},
		PriceStreamPartitions: testPricePartitions,
		AuthStore:             store,
		AuthConfig:            AuthConfig{CookieSecure: false, SessionTTL: time.Hour},
	})

	regBody := []byte(`{"display_name":"Alex","work_email":"alex@company.com","password":"superpass123"}`)
	regReq := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := httptest.NewRecorder()
	r.ServeHTTP(regRec, regReq)
	if regRec.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", regRec.Code, regRec.Body.String())
	}

	loginBody := []byte(`{"work_email":"alex@company.com","password":"superpass123"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	r.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie from login")
	}

	meReq := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	meReq.AddCookie(cookies[0])
	meRec := httptest.NewRecorder()
	r.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", meRec.Code, meRec.Body.String())
	}
	var me AuthUser
	if err := json.Unmarshal(meRec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.WorkEmail != "alex@company.com" {
		t.Fatalf("unexpected me=%+v", me)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	logoutReq.AddCookie(cookies[0])
	logoutRec := httptest.NewRecorder()
	r.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logoutRec.Code, logoutRec.Body.String())
	}

	me2Req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	me2Req.AddCookie(cookies[0])
	me2Rec := httptest.NewRecorder()
	r.ServeHTTP(me2Rec, me2Req)
	if me2Rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout me status=%d body=%s", me2Rec.Code, me2Rec.Body.String())
	}
}

func TestPortfolioRead_ForbiddenForNonOwner(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	store := newFakeAuthStore()
	user := events.UserAccount{
		UserID:       uuid.New(),
		DisplayName:  "Alex",
		WorkEmail:    "alex@company.com",
		PasswordHash: "$2a$10$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuu",
	}
	_, _ = store.CreateUser(context.Background(), user)
	sid := uuid.New()
	_, _ = store.CreateSession(context.Background(), events.UserSession{
		SessionID: sid,
		UserID:    user.UserID,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         &fakeOwnedPortfolioReadStore{fakePortfolioReadStore: fakePortfolioReadStore{found: true}, owned: false},
		PortfolioCatalog:      &fakePortfolioCatalogStore{ownershipOK: false, ownershipSet: true},
		PriceStreamPartitions: testPricePartitions,
		AuthStore:             store,
		AuthConfig:            AuthConfig{CookieSecure: false, SessionTTL: time.Hour},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/portfolios/"+uuid.New().String(), nil)
	req.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: sid.String()})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
